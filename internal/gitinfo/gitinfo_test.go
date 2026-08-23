package gitinfo

import (
	"os/exec"
	"regexp"
	"testing"
)

// initRepo builds a throwaway git repo with one commit and returns its
// path. Skips when git isn't installed (it always is on CI).
func initRepo(t *testing.T) string {
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

func TestCollect(t *testing.T) {
	dir := initRepo(t)
	info, err := Collect(dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(info.HeadSHA) {
		t.Errorf("HeadSHA = %q, want 40-hex", info.HeadSHA)
	}
	if info.Branch != "main" {
		t.Errorf("Branch = %q, want main", info.Branch)
	}
	if info.CommittedAt == nil {
		t.Error("CommittedAt is nil")
	}
	if len(info.Branches) != 1 || info.Branches[0].Name != "main" {
		t.Errorf("Branches = %+v, want [main]", info.Branches)
	}
	// No origin remote in the fixture → RepoURL stays empty, no error.
	if info.RepoURL != "" {
		t.Errorf("RepoURL = %q, want empty", info.RepoURL)
	}
}

func TestCollectNotARepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if _, err := Collect(t.TempDir()); err == nil {
		t.Error("Collect on a non-repo: want error, got nil")
	}
}
