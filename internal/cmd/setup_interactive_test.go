package cmd

import (
	"os"
	"testing"
)

// isTerminal must return false when stdin is a pipe / file (the typical
// CI case). Without this guard, `korva setup` in CI would block on an
// interactive prompt waiting for input that never comes.
func TestIsTerminal_FileIsNotATTY(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "fake-stdin")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer tmp.Close()

	if got := isTerminal(tmp); got {
		t.Errorf("isTerminal(regular file) = true, want false")
	}
}

// Nil stat — exercise the early-return guard so future refactors can't
// accidentally turn a stat error into a panic.
func TestIsTerminal_ClosedFileIsNotATTY(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "fake-stdin")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	_ = f.Close()
	if got := isTerminal(f); got {
		t.Errorf("isTerminal(closed file) = true, want false")
	}
}
