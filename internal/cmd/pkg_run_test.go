package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCmdPkgRun_NoCommand — missing local file returns 1.
func TestCmdPkgRun_NoCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if code := cmdPkgRun([]string{"nonexistent"}); code != 1 {
		t.Errorf("got %d, want 1", code)
	}
}

// TestCmdPkgRun_NoArgs — error path.
func TestCmdPkgRun_NoArgs(t *testing.T) {
	if code := cmdPkgRun(nil); code != 1 {
		t.Errorf("got %d, want 1", code)
	}
}

// TestCmdPkgRun_PrintsBody — when a Claude command file exists,
// `pkg run` reads it, substitutes $ARGUMENTS, and returns 0. We
// capture stdout via a pipe.
func TestCmdPkgRun_PrintsBody(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cmds := filepath.Join(dir, ".claude", "commands")
	if err := os.MkdirAll(cmds, 0o755); err != nil {
		t.Fatalf("mkdir cmds: %v", err)
	}
	body := "---\ndescription: t\n---\n\nrun $ARGUMENTS now\n"
	if err := os.WriteFile(filepath.Join(cmds, "dev.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write cmd: %v", err)
	}

	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Capture stdout. We don't need to assert on substitution detail
	// here (it's covered by editor's golden tests); just that the
	// command returns 0 when the file is present.
	r, w, _ := os.Pipe()
	saved := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = saved })

	if code := cmdPkgRun([]string{"dev", "fix", "auth"}); code != 0 {
		t.Errorf("got %d, want 0", code)
	}
	w.Close()
	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !contains(out, "run fix auth now") {
		t.Errorf("$ARGUMENTS not substituted; output = %q", out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
