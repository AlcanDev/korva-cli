package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlcanDev/korva-cli/internal/api"
)

// TestTranspileClaude_Golden — the Claude transpiler is essentially a
// frontmatter rebuilder. The body should be byte-identical to the
// canonical input minus any existing frontmatter.
func TestTranspileClaude_Golden(t *testing.T) {
	cmd := api.PackageCommand{
		Name:         "dev",
		Description:  "EPSDTAVAO orchestrator",
		ArgumentHint: "<task>",
		Body:         "Run $ARGUMENTS through the orchestrator.\n",
	}
	got := transpileClaude(cmd)
	want := "---\ndescription: EPSDTAVAO orchestrator\nargument-hint: <task>\n---\n\nRun $ARGUMENTS through the orchestrator.\n"
	if got != want {
		t.Errorf("Claude transpile mismatch.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestTranspileCopilot_Golden — Copilot adds `name` + `agent`, rewrites
// `$ARGUMENTS`. The body otherwise survives untouched.
func TestTranspileCopilot_Golden(t *testing.T) {
	cmd := api.PackageCommand{
		Name:         "dev",
		Description:  "EPSDTAVAO orchestrator",
		ArgumentHint: "<task>",
		Body:         "Run $ARGUMENTS through the orchestrator.\n",
	}
	got := transpileCopilot(cmd)
	// Copilot prompt files: description + argument-hint + agent: agent.
	// Filename is the command name (no `name:`). `agent: agent` is the
	// chat MODE; the model (Sonnet, GPT-4o, etc.) is picked at runtime
	// in the chat UI. See transpileCopilot's doc-comment.
	want := "---\ndescription: EPSDTAVAO orchestrator\nargument-hint: <task>\nagent: agent\n---\n\nRun ${input:arguments} through the orchestrator.\n"
	if got != want {
		t.Errorf("Copilot transpile mismatch.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestStripFrontmatter — a body that ALREADY carries a `---` block
// shouldn't grow a second one when we transpile.
func TestStripFrontmatter(t *testing.T) {
	body := "---\ndescription: existing\n---\n\nhello\n"
	got := stripFrontmatter(body)
	if got != "hello\n" {
		t.Errorf("stripFrontmatter = %q, want %q", got, "hello\n")
	}
	// No frontmatter → passthrough.
	if got := stripFrontmatter("plain text\n"); got != "plain text\n" {
		t.Errorf("passthrough: %q", got)
	}
}

// TestYamlEscape — strings with ':' or '"' must be quoted; plain text
// stays bare.
func TestYamlEscape(t *testing.T) {
	cases := map[string]string{
		"hello world":         "hello world",
		"foo: bar":            `"foo: bar"`,
		`he said "hi"`:        `"he said \"hi\""`,
		"line1\nline2":        `"line1\nline2"`, // newline triggers quoting
		"plain-ascii_text-42": "plain-ascii_text-42",
	}
	for in, want := range cases {
		if got := yamlEscape(in); got != want {
			t.Errorf("yamlEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestWritePackage_FullDisk drives WritePackage end-to-end against a
// temp dir, asserts both files exist with the expected frontmatter.
func TestWritePackage_FullDisk(t *testing.T) {
	root := t.TempDir()
	pkg := api.Package{
		Name:    "ci-helpers",
		Version: 1,
		Commands: []api.PackageCommand{
			{Name: "dev", Description: "Orchestrator", Body: "Body 1\n"},
			{Name: "explore", Description: "Understand", Body: "Body 2\n"},
		},
	}
	results, err := WritePackage(root, pkg)
	if err != nil {
		t.Fatalf("WritePackage: %v", err)
	}
	if len(results) != 4 {
		t.Errorf("got %d results, want 4 (2 cmds × 2 editors)", len(results))
	}
	// Validate the four files exist and contain the right markers.
	checks := []struct {
		path    string
		mustSub string
	}{
		{".github/prompts/dev.prompt.md", "agent: agent"},
		{".github/prompts/explore.prompt.md", "agent: agent"},
		{".claude/commands/dev.md", "Body 1"},
		{".claude/commands/explore.md", "Body 2"},
	}
	for _, c := range checks {
		b, err := os.ReadFile(filepath.Join(root, c.path))
		if err != nil {
			t.Errorf("missing file %s: %v", c.path, err)
			continue
		}
		if !strings.Contains(string(b), c.mustSub) {
			t.Errorf("%s missing %q", c.path, c.mustSub)
		}
	}
}

// TestFindProjectRoot — placing a `.git` directory in a temp tree
// returns that tree as the root.
func TestFindProjectRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	sub := filepath.Join(root, "deep", "nested", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	got, err := FindProjectRoot(sub)
	if err != nil {
		t.Fatalf("FindProjectRoot: %v", err)
	}
	// On macOS, /private/var/folders/... gets resolved through symlinks.
	// Comparing via filepath.EvalSymlinks would be cleaner; assert the
	// suffix instead so the test is portable.
	if !strings.HasSuffix(got, filepath.Base(root)) {
		t.Errorf("got %q, expected suffix %q", got, filepath.Base(root))
	}
}

// TestFindProjectRoot_NoMarker — no manifest, no .git, the walk hits
// the filesystem root and returns ErrNoProjectRoot.
func TestFindProjectRoot_NoMarker(t *testing.T) {
	// A subdir of t.TempDir has no .git; walking up to TempDir's
	// ancestors also has none (the harness uses /tmp/.../<random>).
	// But /tmp itself might have a marker on some hosts — use a deeper
	// chain to be safe.
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// We can't assert this universally (a parent might be a real repo),
	// so accept either: a returned path that contains a marker, OR the
	// not-found error.
	got, err := FindProjectRoot(sub)
	if err == nil {
		// Real walk found something — at least make sure a marker is
		// there so the test isn't lying.
		ok := false
		for _, m := range projectMarkers {
			if _, statErr := os.Stat(filepath.Join(got, m)); statErr == nil {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("FindProjectRoot returned %q without a marker", got)
		}
	}
}

// TestRemovePackage_Idempotent — uninstalling twice is fine.
func TestRemovePackage_Idempotent(t *testing.T) {
	root := t.TempDir()
	pkg := api.Package{Commands: []api.PackageCommand{{Name: "dev", Body: "x"}}}
	if _, err := WritePackage(root, pkg); err != nil {
		t.Fatalf("WritePackage: %v", err)
	}
	first, err := RemovePackage(root, []string{"dev"})
	if err != nil {
		t.Fatalf("first remove: %v", err)
	}
	if len(first) != 2 {
		t.Errorf("first remove: got %d, want 2 (copilot + claude)", len(first))
	}
	second, err := RemovePackage(root, []string{"dev"})
	if err != nil {
		t.Fatalf("second remove: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second remove: got %d, want 0 (already gone)", len(second))
	}
}
