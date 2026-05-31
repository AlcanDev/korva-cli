// Package claudehook handles Claude Code's hook events that fire on
// every chat prompt. The CLI ships a tiny Bash script into the user's
// project that `exec`s `korva pkg hook --event=user-prompt-submit`;
// THIS package is what that command runs. Keeping the logic here
// (not in Bash) means JSON parsing, model attribution and HTTP go
// through Go's standard library — no `jq` dependency, no Bash
// portability surprises.
//
// Per ADR 0020:
//   - Best-effort: the hook MUST never block or break the developer's
//     chat. Any error is logged to stderr and the function returns 0
//     so Claude Code shows no warning.
//   - Authentication uses ~/.korva/config.toml (the same file
//     `korva login` writes). Missing config = silent no-op.
//   - Editor identifier on the wire is "claude-code". Combined with
//     the model_id from the hook payload, the backbone has a true
//     per-call attribution that VS Code can't match.
package claudehook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/AlcanDev/korva-cli/internal/config"
)

// payload is the subset of Claude Code's UserPromptSubmit JSON we
// need. The hook protocol is documented at docs.anthropic.com — we
// take only what we use and ignore the rest, which keeps us
// forward-compatible if Anthropic adds more fields.
type payload struct {
	HookEventName  string `json:"hook_event_name"`
	Prompt         string `json:"prompt"`
	Cwd            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path"`
	// Both shapes exist in the wild: an object with `id` (newer
	// builds) and a bare string (older builds). The handler reads
	// whichever is present.
	Model json.RawMessage `json:"model"`
}

// commandRE pulls the slash-command name from "/<name> args…". Mirrors
// the shape Claude Code's prompt files allow. The name must be the
// FIRST token of the prompt — embedded `/dev` inside prose doesn't
// fire telemetry.
var commandRE = regexp.MustCompile(`^/([a-z][a-z0-9-]{0,47})(?:\s|$)`)

// Handle reads a single JSON hook payload from stdin and posts a
// run-telemetry event to the team's backbone when the prompt looks
// like a Korva-installed slash command. Returns nil even on error so
// the CLI exit code is always 0 — failure must never disturb the
// developer's chat.
func Handle(stdin io.Reader) error {
	raw, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		warn("read stdin: %v", err)
		return nil //nolint:nilerr // hook must never propagate errors to Claude Code
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		warn("decode payload: %v", err)
		return nil //nolint:nilerr // hook must never propagate errors to Claude Code
	}
	if p.HookEventName != "" && p.HookEventName != "UserPromptSubmit" {
		// We're registered only on UserPromptSubmit but defend
		// against a future Anthropic change that calls the hook on
		// other events.
		return nil
	}
	m := commandRE.FindStringSubmatch(strings.TrimLeft(p.Prompt, " \t"))
	if m == nil {
		return nil // not a slash command — silently done
	}
	cmdName := m[1]

	// Only fire when the project actually installed this command via
	// Korva. Otherwise an unrelated `.claude/commands/foo.md` the dev
	// hand-wrote would generate telemetry it never asked for.
	if !commandIsInstalled(p.Cwd, cmdName) {
		return nil
	}

	cfg, err := config.Load()
	if err != nil || !cfg.LoggedIn() {
		// Missing login = silent. The dev might just be running
		// Claude Code in a personal project without Korva auth, and
		// we don't want to nag them.
		return nil //nolint:nilerr // hook stays silent on every config error
	}

	post(cfg, runEvent{
		CommandName: cmdName,
		Project:     normalizeProject(p.Cwd),
		Model:       extractModel(p.Model),
		Editor:      "claude-code",
		Succeeded:   true,
	})
	return nil
}

// commandIsInstalled returns true when `.claude/commands/<name>.md`
// exists in the project root. We deliberately don't fall back to
// `.github/prompts/` here — that's the Copilot side, and on Claude
// Code we only count files Claude itself will load.
func commandIsInstalled(cwd, name string) bool {
	if cwd == "" {
		return false
	}
	p := filepath.Join(cwd, ".claude", "commands", name+".md")
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// extractModel handles both observed shapes of the `model` field:
// `{"id":"claude-sonnet-4-5",...}` and bare `"claude-sonnet-4-5"`.
// Returns "" when neither parses — we still record the event, the
// backbone treats empty model as "unknown".
func extractModel(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try object first.
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.ID != "" {
		return obj.ID
	}
	// Fall back to bare string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

// normalizeProject mirrors the helper the backbone uses on observations
// (NormalizeProject in store/observations.go). Lowercased, trimmed
// basename of the cwd. Keeps activity timelines coherent across the
// CLI, the extension and the hook.
func normalizeProject(cwd string) string {
	if cwd == "" {
		return ""
	}
	base := filepath.Base(cwd)
	return strings.ToLower(strings.TrimSpace(base))
}

// runEvent is what we POST to /v1/team/packages/runs. The server
// already accepts package_id / command_id as empty strings (the hook
// doesn't know them), and model is the new Fase 8c field.
type runEvent struct {
	CommandName string
	Project     string
	Model       string
	Editor      string
	Succeeded   bool
}

func post(cfg config.Config, ev runEvent) {
	body, _ := json.Marshal(map[string]any{
		"command_name": ev.CommandName,
		"project":      ev.Project,
		"model":        ev.Model,
		"editor":       ev.Editor,
		"succeeded":    ev.Succeeded,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cfg.ServerURL+"/v1/team/packages/runs",
		strings.NewReader(string(body)))
	if err != nil {
		warn("build request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		warn("post run: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	// We don't surface non-2xx as errors to stderr — the hook is
	// silent on the happy path and we don't want flakey backbones
	// to clutter the user's chat history.
}

func warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "korva-hook: "+format+"\n", args...)
}
