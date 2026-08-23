package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmdContext_Usage — bare `korva context` prints usage, exit 0.
func TestCmdContext_Usage(t *testing.T) {
	if code := cmdContext(nil); code != 0 {
		t.Errorf("cmdContext(nil) = %d, want 0", code)
	}
}

// TestCmdContext_Unknown — unknown subcommand exits 1.
func TestCmdContext_Unknown(t *testing.T) {
	if code := cmdContext([]string{"frobnicate"}); code != 1 {
		t.Errorf("got %d, want 1", code)
	}
}

// TestCmdContextPush_RequiresToken — no KORVA_CONTEXT_TOKEN → exit 1
// before any network hop.
func TestCmdContextPush_RequiresToken(t *testing.T) {
	t.Setenv(pushTokenEnv, "")
	if code := cmdContextPush(nil); code != 1 {
		t.Errorf("got %d, want 1", code)
	}
}

// TestCmdContextPush_RejectsNonKctxToken — a personal API token is
// rejected by design.
func TestCmdContextPush_RejectsNonKctxToken(t *testing.T) {
	t.Setenv(pushTokenEnv, "sometoken")
	if code := cmdContextPush(nil); code != 1 {
		t.Errorf("got %d, want 1", code)
	}
}

// initGitRepo mirrors the gitinfo test fixture: a repo with one commit.
func initGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@korva.test")
	run("config", "user.name", "Test")
	run("commit", "--allow-empty", "-m", "initial")
	return dir
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
}

// TestCmdContextPush_EndToEnd — a push from a real temp repo reaches the
// fake backbone with the collected git state and exits 0.
func TestCmdContextPush_EndToEnd(t *testing.T) {
	dir := initGitRepo(t)
	chdir(t, dir)

	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/context/push" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer kctx_testtoken" {
			t.Errorf("auth = %q", auth)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"project": got["project"], "head_sha": got["head_sha"],
			"branch": "main", "branches": []any{}, "brief_md": "", "stale": false,
		})
	}))
	defer srv.Close()

	t.Setenv(pushTokenEnv, "kctx_testtoken")
	code := cmdContextPush([]string{"--server", srv.URL, "--project", "atlas", "--here"})
	if code != 0 {
		t.Fatalf("push exit = %d, want 0", code)
	}
	if got["project"] != "atlas" || got["branch"] != "main" {
		t.Errorf("payload = %v", got)
	}
	sha, _ := got["head_sha"].(string)
	if len(sha) != 40 {
		t.Errorf("head_sha = %q, want 40-hex", sha)
	}
}

// TestCmdContextPull_EndToEnd — pull writes the three files, adds the
// gitignore entry, and exits 0 (404 on the project record tolerated).
func TestCmdContextPull_EndToEnd(t *testing.T) {
	dir := initGitRepo(t)
	chdir(t, dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/context") && strings.Contains(r.URL.Path, "/projects/"):
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no context"})
		case r.URL.Path == "/v1/team/context":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"team_id": "t1",
				"projects": []map[string]any{{
					"project": "other", "head_sha": strings.Repeat("b", 40),
					"branch": "main", "branches": []any{}, "brief_md": "Other service.", "stale": false,
				}},
			})
		case r.URL.Path == "/v1/team/observations":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"team_id": "t1",
				"entries": []map[string]any{{
					"id": "01OBS", "kind": "decision", "title": "Use slog",
					"content": "Chose slog.", "tags": []string{}, "created_at": "2026-08-20T10:00:00Z",
				}},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Fake a logged-in CLI: config.json in a temp KORVA_HOME.
	home := t.TempDir()
	t.Setenv("KORVA_HOME", home)
	cfg := `{"server_url":"` + srv.URL + `","token":"user-token"}`
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	code := cmdContextPull([]string{"--project", "atlas", "--here"})
	if code != 0 {
		t.Fatalf("pull exit = %d, want 0", code)
	}

	for _, rel := range []string{
		".korva/context/portfolio.md",
		".korva/context/project-brief.md",
		".korva/context/decisions.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	brief, _ := os.ReadFile(filepath.Join(dir, ".korva/context/project-brief.md"))
	if !strings.Contains(string(brief), "no context record yet") {
		t.Errorf("brief should carry the wiring hint:\n%s", brief)
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gi), ".korva/") {
		t.Errorf(".gitignore missing .korva/ entry: %q", gi)
	}
}
