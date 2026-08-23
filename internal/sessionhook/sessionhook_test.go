package sessionhook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlcanDev/korva-cli/internal/api"
	"github.com/AlcanDev/korva-cli/internal/contextfiles"
)

// seedCache writes a real .korva/context/ cache into root.
func seedCache(t *testing.T, root string) {
	t.Helper()
	pushed := time.Now().Add(-2 * time.Hour)
	_, err := contextfiles.Write(root, contextfiles.Data{
		ServerURL: "https://test.korva.dev",
		Project:   "atlas",
		Context: &api.ProjectContext{
			Project: "atlas", HeadSHA: strings.Repeat("a", 40), Branch: "main",
			PushedAt: &pushed, BriefMD: "Atlas is the billing service.",
		},
		Portfolio: []api.ProjectContext{
			{Project: "atlas", HeadSHA: strings.Repeat("a", 40), Branch: "main", PushedAt: &pushed, BriefMD: "Atlas is the billing service."},
			{Project: "lyra"},
		},
		Observations: []api.Observation{
			{ID: "01OBS", Kind: "decision", Title: "Use slog", Content: "Stdlib alignment."},
		},
		GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("seed cache: %v", err)
	}
}

func handle(t *testing.T, p map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(p)
	var out bytes.Buffer
	if err := Handle(bytes.NewReader(raw), &out); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	return out.String()
}

// TestSessionStartInjectsCachedContext — fresh cache → compact block on
// stdout, no network involved.
func TestSessionStartInjectsCachedContext(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KORVA_HOME", t.TempDir()) // not logged in → no refresh attempt
	seedCache(t, root)

	out := handle(t, map[string]any{"hook_event_name": "SessionStart", "cwd": root})
	for _, want := range []string{
		"<korva-context>",
		"Atlas is the billing service.",
		"`01OBS`",
		"## Portfolio",
		"- lyra",
		"vault_get",
		"</korva-context>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("inject missing %q:\n%s", want, out)
		}
	}
}

// TestSessionStartNoCacheNoLogin — nothing cached and no way to fetch →
// silent no-op, never an error into the session.
func TestSessionStartNoCacheNoLogin(t *testing.T) {
	t.Setenv("KORVA_HOME", t.TempDir())
	out := handle(t, map[string]any{"hook_event_name": "SessionStart", "cwd": t.TempDir()})
	if out != "" {
		t.Errorf("want silent no-op, got: %q", out)
	}
}

// TestRenderInjectCapsBrief — a giant brief is cut at the line cap with
// a continuation marker instead of flooding the session.
func TestRenderInjectCapsBrief(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("line of brief text\n", 200)
	_, err := contextfiles.Write(root, contextfiles.Data{
		ServerURL: "x", Project: "atlas",
		Context:     &api.ProjectContext{Project: "atlas", BriefMD: long},
		GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, ok := RenderInject(root)
	if !ok {
		t.Fatal("want ok")
	}
	if got := strings.Count(out, "line of brief text"); got > maxBriefLines {
		t.Errorf("brief lines = %d, want ≤ %d", got, maxBriefLines)
	}
	if !strings.Contains(out, "…") {
		t.Error("truncated brief should carry a continuation marker")
	}
}

func writeTranscript(t *testing.T, lines int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	body := strings.Repeat("{\"type\":\"turn\"}\n", lines)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestStopNudgesOncePerSession — the full gate chain: big transcript →
// block JSON once; same session again → silence; stop_hook_active →
// silence; tiny transcript → silence.
func TestStopNudgesOncePerSession(t *testing.T) {
	t.Setenv("KORVA_HOME", t.TempDir())
	big := writeTranscript(t, minTranscriptLines+10)

	out := handle(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "sess-123", "transcript_path": big,
	})
	var decision struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &decision); err != nil {
		t.Fatalf("stop output is not JSON: %q", out)
	}
	if decision.Decision != "block" || !strings.Contains(decision.Reason, "vault_save") {
		t.Errorf("unexpected decision: %+v", decision)
	}

	// Same session again → marker suppresses a second nudge.
	if out := handle(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "sess-123", "transcript_path": big,
	}); out != "" {
		t.Errorf("second stop must be silent, got %q", out)
	}

	// stop_hook_active → the agent already resumed once; let it finish.
	if out := handle(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "sess-999",
		"transcript_path": big, "stop_hook_active": true,
	}); out != "" {
		t.Errorf("stop_hook_active must be silent, got %q", out)
	}

	// Trivial session → no nudge, no marker spend.
	small := writeTranscript(t, 3)
	if out := handle(t, map[string]any{
		"hook_event_name": "Stop", "session_id": "sess-tiny", "transcript_path": small,
	}); out != "" {
		t.Errorf("small transcript must be silent, got %q", out)
	}
}

// TestHandleGarbageInput — malformed payloads are swallowed silently.
func TestHandleGarbageInput(t *testing.T) {
	var out bytes.Buffer
	if err := Handle(strings.NewReader("not json"), &out); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("garbage input must be silent, got %q", out.String())
	}
}

// TestEnsureHooksWiresBothEvents — script on disk + SessionStart and
// Stop registrations, idempotent on the second run.
func TestEnsureHooksWiresBothEvents(t *testing.T) {
	root := t.TempDir()
	if err := EnsureHooks(root); err != nil {
		t.Fatalf("EnsureHooks: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "hooks", HookFileName)); err != nil {
		t.Fatalf("script missing: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings missing: %v", err)
	}
	var doc struct {
		Hooks map[string][]any `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("settings not JSON: %v", err)
	}
	for _, ev := range []string{"SessionStart", "Stop"} {
		if len(doc.Hooks[ev]) != 1 {
			t.Errorf("%s entries = %d, want 1", ev, len(doc.Hooks[ev]))
		}
	}

	// Idempotent.
	if err := EnsureHooks(root); err != nil {
		t.Fatalf("second EnsureHooks: %v", err)
	}
	raw2, _ := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if !bytes.Equal(raw, raw2) {
		t.Error("second install changed settings.json")
	}
}
