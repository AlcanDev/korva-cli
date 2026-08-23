package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRulesBlockHasProactiveCues(t *testing.T) {
	b := rulesBlock()
	for _, want := range []string{"vault_context", "vault_search", "vault_save", "vault_get", "team_portfolio", "topic_key", ".korva/context/project-brief.md", "skill_*"} {
		if !strings.Contains(b, want) {
			t.Errorf("rules block missing %q", want)
		}
	}
	// The "save to memory" → vault-in-parallel guidance must be present so
	// agents don't only write editor-local memory for team decisions.
	if !strings.Contains(b, "save to memory") || !strings.Contains(b, "in parallel") {
		t.Errorf("rules block missing the save-to-memory/parallel guidance")
	}
	if !strings.Contains(b, rulesStartMarker) || !strings.Contains(b, rulesEndMarker) {
		t.Error("rules block missing managed markers")
	}
}

// TestRulesBlockStaleBriefIsAskFirst — the stale-brief guidance (F2-lite,
// platform ADR-0027) must ask before writing anywhere, ask at most once
// per session, and never claim Korva writes to GitHub on the agent's
// behalf: no LLM worker, no auto-PR, human confirms every write.
func TestRulesBlockStaleBriefIsAskFirst(t *testing.T) {
	b := rulesBlock()
	for _, want := range []string{
		"Stale brief",
		"ASK, don't auto-write",
		"ONCE per session",
		"decisions.md",
		"Never call that endpoint or edit README.md yourself without an explicit yes",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("rules block missing stale-brief guidance %q", want)
		}
	}
}

func TestUpsertManagedBlockCreatesUpdatesAndPreserves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "CLAUDE.md")

	// Create in a non-existent nested dir.
	if action, err := upsertManagedBlock(path, rulesBlock()); err != nil || action != "created" {
		t.Fatalf("create: action=%q err=%v", action, err)
	}

	// Re-installing the identical block is a no-op.
	if action, err := upsertManagedBlock(path, rulesBlock()); err != nil || action != "unchanged" {
		t.Fatalf("idempotent: action=%q err=%v", action, err)
	}

	// Pre-existing user content around the block must be preserved on update.
	custom := "# My rules\nAlways write tests.\n\n" + rulesBlock() + "\nTrailing note.\n"
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	newBlock := rulesStartMarker + "\nUPDATED\n" + rulesEndMarker
	if action, err := upsertManagedBlock(path, newBlock); err != nil || action != "updated" {
		t.Fatalf("update: action=%q err=%v", action, err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, "# My rules") || !strings.Contains(s, "Trailing note.") {
		t.Errorf("update clobbered surrounding content:\n%s", s)
	}
	if !strings.Contains(s, "UPDATED") || strings.Contains(s, "vault_context") {
		t.Errorf("update did not replace the managed block:\n%s", s)
	}
	// Exactly one managed block remains.
	if n := strings.Count(s, rulesStartMarker); n != 1 {
		t.Errorf("expected 1 managed block, found %d", n)
	}
}

func TestUpsertManagedBlockAppendsToFileWithoutMarkers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "global_rules.md")
	if err := os.WriteFile(path, []byte("# Existing\nkeep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if action, err := upsertManagedBlock(path, rulesBlock()); err != nil || action != "appended" {
		t.Fatalf("append: action=%q err=%v", action, err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, "keep me") || !strings.Contains(s, rulesStartMarker) {
		t.Errorf("append did not preserve content + add block:\n%s", s)
	}
}

// TestRulesBlockHasNoStaleTools — the block must never re-install
// references to tools removed from the platform (ADR-0022 dropped
// team_pipeline_state; the block kept advertising it until this test).
func TestRulesBlockHasNoStaleTools(t *testing.T) {
	if strings.Contains(rulesBlock(), "team_pipeline_state") {
		t.Error("rules block references the removed team_pipeline_state tool")
	}
}

// TestInstallProjectRules — --install --project writes the managed
// block into every per-repo instruction file, idempotently.
func TestInstallProjectRules(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if code := cmdRules([]string{"--install", "--project", "--here"}); code != 0 {
		t.Fatalf("install --project = %d, want 0", code)
	}
	for _, rel := range []string{
		".github/copilot-instructions.md",
		".cursor/rules/korva.mdc",
		".windsurfrules",
		"AGENTS.md",
	} {
		raw, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Errorf("missing %s: %v", rel, err)
			continue
		}
		if !strings.Contains(string(raw), ".korva/context/project-brief.md") {
			t.Errorf("%s lacks the session-start directive", rel)
		}
	}

	// Idempotent: a second run reports success and changes nothing.
	before, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if code := cmdRules([]string{"--install", "--project", "--here"}); code != 0 {
		t.Fatalf("second install = %d, want 0", code)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if string(before) != string(after) {
		t.Error("second install changed AGENTS.md")
	}
}
