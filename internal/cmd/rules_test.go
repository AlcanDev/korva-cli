package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRulesBlockHasProactiveCues(t *testing.T) {
	b := rulesBlock()
	for _, want := range []string{"vault_context", "team_pipeline_state", "vault_search", "vault_save", "skill_*"} {
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
