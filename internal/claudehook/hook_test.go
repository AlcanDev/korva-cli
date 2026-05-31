package claudehook

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AlcanDev/korva-cli/internal/config"
)

// withTempHome points config.Load() at a throwaway HOME so tests can
// stage a fake `~/.korva/config.toml`. Returns the path so the test
// can read what the helper wrote.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

// stageLogin writes a minimal config.toml so config.LoggedIn() returns
// true. serverURL is what the hook POSTs against.
func stageLogin(t *testing.T, home, serverURL string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, ".korva"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := config.Config{ServerURL: serverURL, Token: "test-token"}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
}

// stageCommand drops a fake `.claude/commands/<name>.md` so the hook's
// install-guard accepts the prompt.
func stageCommand(t *testing.T, cwd, name string) {
	t.Helper()
	dir := filepath.Join(cwd, ".claude", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir cmds: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte("body"), 0o644); err != nil {
		t.Fatalf("write cmd: %v", err)
	}
}

// TestHandle_PostsRunOnInstalledCommand drives the full path: a
// well-formed UserPromptSubmit, an installed command, an authed
// login. We assert the backbone receives the right payload.
func TestHandle_PostsRunOnInstalledCommand(t *testing.T) {
	home := withTempHome(t)
	projectDir := filepath.Join(home, "proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	stageCommand(t, projectDir, "dev")

	var got struct {
		ModelSeen   string
		CommandSeen string
		EditorSeen  string
	}
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model       string `json:"model"`
			CommandName string `json:"command_name"`
			Editor      string `json:"editor"`
		}
		_ = json.Unmarshal(raw, &body)
		got.ModelSeen = body.Model
		got.CommandSeen = body.CommandName
		got.EditorSeen = body.Editor
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	stageLogin(t, home, srv.URL)

	payload := map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"prompt":          "/dev fix the auth bug",
		"cwd":             projectDir,
		"model":           map[string]any{"id": "claude-sonnet-4-5"},
	}
	raw, _ := json.Marshal(payload)
	if err := Handle(strings.NewReader(string(raw))); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("backbone hits = %d, want 1", hits)
	}
	if got.CommandSeen != "dev" {
		t.Errorf("command = %q", got.CommandSeen)
	}
	if got.ModelSeen != "claude-sonnet-4-5" {
		t.Errorf("model = %q", got.ModelSeen)
	}
	if got.EditorSeen != "claude-code" {
		t.Errorf("editor = %q", got.EditorSeen)
	}
}

// TestHandle_SilentOnNonSlashPrompt — a plain English prompt does
// not fire telemetry. Defends the dev's privacy: only declared
// slash-commands should ever be observed.
func TestHandle_SilentOnNonSlashPrompt(t *testing.T) {
	home := withTempHome(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	t.Cleanup(srv.Close)
	stageLogin(t, home, srv.URL)

	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"prompt":          "what does this function do?",
		"cwd":             home,
	})
	if err := Handle(strings.NewReader(string(payload))); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Errorf("hits = %d, want 0", hits)
	}
}

// TestHandle_SilentOnUnknownCommand — a slash-prompt for a command
// the project never installed via Korva is ignored. Otherwise a
// hand-written `.claude/commands/foo.md` would leak telemetry the
// dev never consented to.
func TestHandle_SilentOnUnknownCommand(t *testing.T) {
	home := withTempHome(t)
	projectDir := filepath.Join(home, "proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	// NO stageCommand call — the command is not installed.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	t.Cleanup(srv.Close)
	stageLogin(t, home, srv.URL)

	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"prompt":          "/notinstalled foo",
		"cwd":             projectDir,
	})
	if err := Handle(strings.NewReader(string(payload))); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Errorf("hits = %d, want 0", hits)
	}
}

// TestHandle_SilentWhenNotLoggedIn — no token = no telemetry. Lets
// a dev use Claude Code in a personal project without our backbone.
func TestHandle_SilentWhenNotLoggedIn(t *testing.T) {
	home := withTempHome(t)
	projectDir := filepath.Join(home, "proj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	stageCommand(t, projectDir, "dev")
	// Intentionally NOT calling stageLogin.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	t.Cleanup(srv.Close)
	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"prompt":          "/dev hi",
		"cwd":             projectDir,
	})
	if err := Handle(strings.NewReader(string(payload))); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Errorf("hits = %d, want 0", hits)
	}
}

// TestExtractModel — both observed shapes are honored.
func TestExtractModel(t *testing.T) {
	cases := map[string]string{
		`{"id":"claude-sonnet-4-5","display_name":"Sonnet"}`: "claude-sonnet-4-5",
		`"claude-3-5-opus"`: "claude-3-5-opus",
		`null`:              "",
		``:                  "",
	}
	for in, want := range cases {
		got := extractModel(json.RawMessage(in))
		if got != want {
			t.Errorf("extractModel(%q) = %q, want %q", in, got, want)
		}
	}
}
