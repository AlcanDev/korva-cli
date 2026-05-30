package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCmdPkg_Usage — bare `korva pkg` prints usage and returns 0.
func TestCmdPkg_Usage(t *testing.T) {
	if code := cmdPkg(nil); code != 0 {
		t.Errorf("cmdPkg(nil) = %d, want 0", code)
	}
}

// TestCmdPkg_Unknown — an unknown subcommand returns 1 and writes to
// stderr. We don't capture stderr (the harness can), only the exit
// code.
func TestCmdPkg_Unknown(t *testing.T) {
	if code := cmdPkg([]string{"frobnicate"}); code != 1 {
		t.Errorf("cmdPkg(frobnicate) = %d, want 1", code)
	}
}

// TestCmdPkgInstall_RejectsNonKvpCode — installs only proceed for
// `kvp_…` codes (other formats are rejected before any network hop).
func TestCmdPkgInstall_RejectsNonKvpCode(t *testing.T) {
	if code := cmdPkgInstall([]string{"notaprefix"}); code != 1 {
		t.Errorf("got %d, want 1", code)
	}
}

// TestCmdPkgInstall_NoArgs — error path.
func TestCmdPkgInstall_NoArgs(t *testing.T) {
	if code := cmdPkgInstall(nil); code != 1 {
		t.Errorf("got %d, want 1", code)
	}
}

// TestCmdPkgStatus_NoCommands — running status in an empty tempdir
// returns 0 and reports "No commands installed".
func TestCmdPkgStatus_NoCommands(t *testing.T) {
	dir := t.TempDir()
	// Drop a .git so FindProjectRoot picks dir as root.
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if code := cmdPkgStatus(nil); code != 0 {
		t.Errorf("cmdPkgStatus = %d, want 0", code)
	}
}
