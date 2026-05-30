// Project writers: render a canonical command body into editor-specific
// slash-command files under the current project root. Sibling to
// targets.go (which writes JSON MCP configs into HOME); kept separate
// because the file format, target path, and lifecycle are different.
//
// Two formats today:
//
//   - **Claude Code** — .claude/commands/<name>.md, frontmatter has
//     `description` + `argument-hint`, body uses `$ARGUMENTS`.
//   - **Copilot prompt files** — .github/prompts/<name>.prompt.md,
//     frontmatter adds `name` + `agent`, variables use
//     `${input:arguments}`. See docs/specs/team-packages.md.
//
// A future Copilot schema change is a non-breaking bump: introduce a new
// transpiler version (copilotFormatV1 -> V2) without touching the
// canonical body.

package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AlcanDev/korva-cli/internal/api"
)

// ProjectTarget describes one editor file format the CLI writes into
// the project root on `korva pkg install`.
type ProjectTarget struct {
	// Name is the user-facing identifier ("copilot", "claude-code").
	Name string
	// SubPath is the directory under the project root where files land
	// (e.g. ".github/prompts").
	SubPath string
	// Extension is appended to the command name. Copilot uses
	// ".prompt.md"; Claude Code uses ".md".
	Extension string
	// Transpile renders the canonical PackageCommand into the
	// editor-specific file body (frontmatter + body, ready to write).
	Transpile func(api.PackageCommand) string
}

// copilotFormatV1 captures the frontmatter shape the Copilot prompt-file
// system expects today. Bump this constant when Copilot evolves; the
// transpiler is small enough that supporting a second version is a
// switch on this string rather than a rewrite.
const copilotFormatV1 = "v1"

// argumentsRE rewrites Claude's `$ARGUMENTS` placeholder to Copilot's
// `${input:arguments}` variable. Word-boundary on both sides so we
// don't mangle an unrelated `$ARGUMENTSX` token (none exist today, but
// the discipline keeps the transpiler honest).
var argumentsRE = regexp.MustCompile(`\$ARGUMENTS\b`)

// ProjectTargets returns the two writers the install command uses, in
// install priority order (Copilot first per ADR 0019).
func ProjectTargets() []ProjectTarget {
	return []ProjectTarget{
		{
			Name:      "copilot",
			SubPath:   filepath.Join(".github", "prompts"),
			Extension: ".prompt.md",
			Transpile: transpileCopilot,
		},
		{
			Name:      "claude-code",
			SubPath:   filepath.Join(".claude", "commands"),
			Extension: ".md",
			Transpile: transpileClaude,
		},
	}
}

// transpileClaude is essentially a passthrough — the canonical body is
// already in Claude Code's format. We rebuild the frontmatter so a
// command that came in without a frontmatter block (rare, but the
// server doesn't enforce it) still ends up valid.
func transpileClaude(cmd api.PackageCommand) string {
	var b strings.Builder
	b.WriteString("---\n")
	if cmd.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", yamlEscape(cmd.Description))
	}
	if cmd.ArgumentHint != "" {
		fmt.Fprintf(&b, "argument-hint: %s\n", yamlEscape(cmd.ArgumentHint))
	}
	b.WriteString("---\n\n")
	b.WriteString(stripFrontmatter(cmd.Body))
	return b.String()
}

// transpileCopilot builds the Copilot prompt-file format: name +
// description + argument-hint + agent, with `$ARGUMENTS` rewritten.
func transpileCopilot(cmd api.PackageCommand) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", yamlEscape(cmd.Name))
	if cmd.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", yamlEscape(cmd.Description))
	}
	if cmd.ArgumentHint != "" {
		fmt.Fprintf(&b, "argument-hint: %s\n", yamlEscape(cmd.ArgumentHint))
	}
	b.WriteString("agent: gpt-4\n")
	b.WriteString("---\n\n")
	body := stripFrontmatter(cmd.Body)
	body = argumentsRE.ReplaceAllString(body, "${input:arguments}")
	b.WriteString(body)
	return b.String()
}

// stripFrontmatter trims a leading `---\n…\n---\n` block from a
// canonical body so we don't double up frontmatter when the server
// stored a body that already had one.
func stripFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	end := strings.Index(body[4:], "\n---")
	if end < 0 {
		return body
	}
	after := body[4+end+4:]
	// Trim ALL leading newlines (the closing `---` is usually followed
	// by `\n\n` to separate frontmatter from prose; we don't want any
	// of those bleeding through and creating a double blank).
	return strings.TrimLeft(after, "\n")
}

// yamlEscape quotes a value if it contains characters that would
// otherwise break YAML parsing. We deliberately keep it minimal: the
// frontmatter fields here are short single-line strings.
func yamlEscape(s string) string {
	if strings.ContainsAny(s, `:#'"`+"\n") {
		// Escape backslashes + double quotes + newlines, wrap in double
		// quotes. Newlines become `\n` inside the quoted YAML scalar
		// (YAML's double-quoted scalar permits the `\n` escape).
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		escaped = strings.ReplaceAll(escaped, "\n", `\n`)
		return `"` + escaped + `"`
	}
	return s
}

// FindProjectRoot walks up from start looking for any of these
// markers — .git, package.json, go.mod, pyproject.toml, Cargo.toml. The
// walk caps at 8 levels so a stray invocation from somewhere deep in
// HOME doesn't recurse forever. Returns ErrNoProjectRoot when none
// found (the caller prints a loud error so the dev knows to `cd` first
// or pass `--here`).
var ErrNoProjectRoot = errors.New("not a project root (no .git or known manifest found — cd into your project first, or pass --here)")

// projectMarkers are the files/dirs whose presence marks a project
// root. .git first because it's the most common and cheapest to check.
var projectMarkers = []string{
	".git",
	"package.json",
	"go.mod",
	"pyproject.toml",
	"Cargo.toml",
}

// FindProjectRoot ascends up to 8 levels from start, returning the
// nearest directory that contains any projectMarkers. start should
// generally be the cwd.
func FindProjectRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve start: %w", err)
	}
	dir := abs
	for i := 0; i < 8; i++ {
		for _, m := range projectMarkers {
			if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached filesystem root
			break
		}
		dir = parent
	}
	return "", ErrNoProjectRoot
}

// InstallResult is what WritePackage returns for the install command
// to report back to the user (and to the install telemetry payload).
type InstallResult struct {
	Editor string // "copilot" | "claude-code"
	Path   string // relative to root
}

// WritePackage materializes a package's commands into the project root
// for every ProjectTarget. The caller decides root (via FindProjectRoot
// or --here). Returns the per-file write results; any IO error aborts
// the rest of the writes so the project isn't left half-installed.
func WritePackage(root string, pkg api.Package) ([]InstallResult, error) {
	var out []InstallResult
	for _, t := range ProjectTargets() {
		dir := filepath.Join(root, t.SubPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return out, fmt.Errorf("mkdir %s: %w", dir, err)
		}
		for _, cmd := range pkg.Commands {
			rel := filepath.Join(t.SubPath, cmd.Name+t.Extension)
			path := filepath.Join(root, rel)
			body := t.Transpile(cmd)
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				return out, fmt.Errorf("write %s: %w", path, err)
			}
			out = append(out, InstallResult{Editor: t.Name, Path: rel})
		}
	}
	return out, nil
}

// RemovePackage deletes the files a previous WritePackage would have
// written. Silent on already-missing files so an uninstall after a
// partial install is idempotent. Returns the relative paths it removed
// so the CLI can report something honest.
func RemovePackage(root string, commandNames []string) ([]string, error) {
	var removed []string
	for _, t := range ProjectTargets() {
		for _, name := range commandNames {
			rel := filepath.Join(t.SubPath, name+t.Extension)
			path := filepath.Join(root, rel)
			err := os.Remove(path)
			if err == nil {
				removed = append(removed, rel)
				continue
			}
			if os.IsNotExist(err) {
				continue
			}
			return removed, fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return removed, nil
}

// CopilotFormatVersion exposes the constant so tests + telemetry can
// assert the transpiler version the CLI shipped at.
func CopilotFormatVersion() string { return copilotFormatV1 }
