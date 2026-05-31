package claudehook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureHook_FreshProject — install on a clean tree writes both
// the script and the settings.json registration.
func TestEnsureHook_FreshProject(t *testing.T) {
	root := t.TempDir()
	if err := EnsureHook(root); err != nil {
		t.Fatalf("EnsureHook: %v", err)
	}
	scriptPath := filepath.Join(root, ".claude", "hooks", HookFileName)
	b, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	if !strings.Contains(string(b), "exec korva pkg hook") {
		t.Errorf("script body missing exec line: %q", string(b))
	}
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("script not executable: %v", info.Mode())
	}

	settings, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(settings, &doc); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	hooks, _ := doc["hooks"].(map[string]any)
	ups, _ := hooks["UserPromptSubmit"].([]any)
	if len(ups) != 1 {
		t.Fatalf("UserPromptSubmit len = %d, want 1", len(ups))
	}
}

// TestEnsureHook_Idempotent — running install twice must NOT add a
// second hook entry.
func TestEnsureHook_Idempotent(t *testing.T) {
	root := t.TempDir()
	if err := EnsureHook(root); err != nil {
		t.Fatalf("first EnsureHook: %v", err)
	}
	if err := EnsureHook(root); err != nil {
		t.Fatalf("second EnsureHook: %v", err)
	}
	settings, _ := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	var doc map[string]any
	_ = json.Unmarshal(settings, &doc)
	hooks, _ := doc["hooks"].(map[string]any)
	ups, _ := hooks["UserPromptSubmit"].([]any)
	if len(ups) != 1 {
		t.Errorf("UserPromptSubmit len = %d, want 1 after two installs", len(ups))
	}
}

// TestEnsureHook_PreservesExistingKeys — an existing settings.json
// with unrelated keys keeps them intact.
func TestEnsureHook_PreservesExistingKeys(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := map[string]any{
		"permissions": map[string]any{"allow": []any{"read"}},
		"hooks": map[string]any{
			"Stop": []any{map[string]any{"matcher": "", "hooks": []any{}}},
		},
	}
	raw, _ := json.Marshal(existing)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), raw, 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	if err := EnsureHook(root); err != nil {
		t.Fatalf("EnsureHook: %v", err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	if _, ok := doc["permissions"]; !ok {
		t.Error("permissions key was clobbered")
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if _, ok := hooks["Stop"]; !ok {
		t.Error("Stop hook entry was clobbered")
	}
	if _, ok := hooks["UserPromptSubmit"]; !ok {
		t.Error("UserPromptSubmit not added")
	}
}

// TestEnsureHook_RefusesCorruptSettings — a non-JSON settings.json
// is a sign of a hand-edit gone wrong; refuse to overwrite.
func TestEnsureHook_RefusesCorruptSettings(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("seed corrupt: %v", err)
	}
	err := EnsureHook(root)
	if err == nil {
		t.Fatal("EnsureHook accepted corrupt settings.json; expected error")
	}
}
