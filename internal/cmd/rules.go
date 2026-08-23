package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Managed-block markers let `korva rules --install` upsert its block
// idempotently inside files that may hold other user content.
const (
	rulesStartMarker = "<!-- korva:rules:start -->"
	rulesEndMarker   = "<!-- korva:rules:end -->"
)

// rulesBlock is the standing instruction that makes an AI agent use the
// Korva MCP tools proactively. It's plain prose that reads fine in any
// editor's rules/instructions file, wrapped in managed markers so it can
// be re-installed or removed cleanly.
func rulesBlock() string {
	return rulesStartMarker + `
# Korva — team memory & standards (use proactively)

Session start — ALWAYS, in every AI chat:
- If ` + "`.korva/context/project-brief.md`" + ` exists in the workspace, read it and ` + "`.korva/context/decisions.md`" + ` before planning anything — that IS the team's cached context (refresh with ` + "`korva context pull`" + ` when older than ~30 min). Otherwise call ` + "`vault_context`" + ` once to load recent team knowledge.

Use the Korva MCP without being asked:
- Before solving a problem or making a design choice, call ` + "`vault_search`" + ` for prior decisions and fixes. Results are compact — drill into a hit with ` + "`vault_get <id>`" + `.
- After a non-trivial decision, fix, or gotcha, call ` + "`vault_save`" + ` (what, why, alternatives; add a stable ` + "`topic_key`" + ` like ` + "`architecture/auth-model`" + ` for topics that evolve). Never save secrets or raw source. This includes requests to "save to memory" / remember something for the team: record it in the Korva vault in parallel with any editor-local memory (local memory is per-machine; the Korva vault is shared with the team).
- When work references the team's other repos, call ` + "`team_portfolio`" + ` once for the portfolio map.
- Prefer team ` + "`skill_*`" + ` tools over improvising prompts.
` + rulesEndMarker
}

// fileTarget is an editor whose global rules live in a writable file.
type fileTarget struct {
	name string
	rel  []string // path segments under the home dir
}

func rulesFileTargets() []fileTarget {
	return []fileTarget{
		{name: "claude-code", rel: []string{".claude", "CLAUDE.md"}},
		{name: "windsurf", rel: []string{".codeium", "windsurf", "memories", "global_rules.md"}},
	}
}

// upsertManagedBlock writes block into path, replacing an existing managed
// block (matched by markers) or appending one. Parent dirs are created. It
// returns a short verb describing what happened, for user feedback.
func upsertManagedBlock(path, block string) (string, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	if len(existing) == 0 {
		if err := os.WriteFile(path, []byte(block+"\n"), 0o644); err != nil {
			return "", err
		}
		return "created", nil
	}

	text := string(existing)
	start := strings.Index(text, rulesStartMarker)
	end := strings.Index(text, rulesEndMarker)
	if start >= 0 && end > start {
		updated := text[:start] + block + text[end+len(rulesEndMarker):]
		if updated == text {
			return "unchanged", nil
		}
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			return "", err
		}
		return "updated", nil
	}

	// No managed block yet: append, keeping prior content intact.
	sep := "\n\n"
	if strings.HasSuffix(text, "\n") {
		sep = "\n"
	}
	if err := os.WriteFile(path, []byte(text+sep+block+"\n"), 0o644); err != nil {
		return "", err
	}
	return "appended", nil
}

// projectRulesTargets are the per-repo instruction files editors load
// on EVERY request — the deterministic layer for editors without a hook
// system (VS Code Copilot first among them, per the user priority in
// ADR-0019/0026).
func projectRulesTargets() []fileTarget {
	return []fileTarget{
		{name: "copilot", rel: []string{".github", "copilot-instructions.md"}},
		{name: "cursor", rel: []string{".cursor", "rules", "korva.mdc"}},
		{name: "windsurf", rel: []string{".windsurfrules"}},
		{name: "agents-md", rel: []string{"AGENTS.md"}},
	}
}

// installProjectRules upserts the managed block into the current repo's
// always-loaded instruction files, making Korva usage deterministic for
// every AI chat that reads them (Copilot loads .github/copilot-
// instructions.md on every request).
func installProjectRules(here bool) int {
	root, ok := resolveRoot(here)
	if !ok {
		return 1
	}
	block := rulesBlock()
	failures := 0
	for _, t := range projectRulesTargets() {
		path := filepath.Join(append([]string{root}, t.rel...)...)
		action, err := upsertManagedBlock(path, block)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %-12s %v\n", t.name, err)
			failures++
			continue
		}
		fmt.Printf("✓ %-12s %s (%s)\n", t.name, path, action)
	}
	fmt.Println("\nEvery AI chat that reads these files now loads the Korva rules on each request.")
	fmt.Println("For Claude Code, `korva context hook install` adds the fully automatic session hooks on top.")
	if failures > 0 {
		return 1
	}
	return 0
}

// cmdRules prints the proactive-usage rule block (and where each editor keeps
// its global rules), or installs it into the file-based editors with
// --install. Editors whose global rules aren't a writable file (VS Code,
// Cursor) get printed manual instructions instead.
func cmdRules(args []string) int {
	fs := flag.NewFlagSet("rules", flag.ContinueOnError)
	install := fs.Bool("install", false, "write the rule block into file-based editors' global rules")
	project := fs.Bool("project", false, "with --install: write the block into THIS repo's always-loaded instruction files (Copilot, Cursor, Windsurf, AGENTS.md) instead of the global ones")
	here := fs.Bool("here", false, "with --project: use cwd as the project root without walking up")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *install && *project {
		return installProjectRules(*here)
	}

	if !*install {
		fmt.Println("Add this to your editor's user/global rules so the agent uses Korva proactively:")
		fmt.Println()
		fmt.Println(rulesBlock())
		fmt.Println()
		fmt.Println("Where each editor keeps global rules:")
		fmt.Println("  • VS Code:     setting github.copilot.chat.codeGeneration.instructions (user settings.json)")
		fmt.Println("  • Claude Code: ~/.claude/CLAUDE.md")
		fmt.Println("  • Cursor:      Settings → Rules for AI")
		fmt.Println("  • Windsurf:    ~/.codeium/windsurf/memories/global_rules.md")
		fmt.Println("\nRun `korva rules --install` to write it for Claude Code and Windsurf automatically,")
		fmt.Println("or `korva rules --install --project` to write it into THIS repo's instruction files")
		fmt.Println("(.github/copilot-instructions.md, .cursor/rules/, .windsurfrules, AGENTS.md).")
		fmt.Println("Docs: https://platform.korva.dev/docs/editor-setup")
		return 0
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not resolve home dir: %v\n", err)
		return 1
	}

	block := rulesBlock()
	failures := 0
	for _, t := range rulesFileTargets() {
		path := filepath.Join(append([]string{home}, t.rel...)...)
		action, err := upsertManagedBlock(path, block)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %-12s %v\n", t.name, err)
			failures++
			continue
		}
		fmt.Printf("✓ %-12s %s (%s)\n", t.name, path, action)
	}

	fmt.Println("\nVS Code and Cursor keep global rules outside a writable file — add the block manually:")
	fmt.Println("  • VS Code: github.copilot.chat.codeGeneration.instructions in user settings.json")
	fmt.Println("  • Cursor:  Settings → Rules for AI")
	fmt.Println("Run `korva rules` (no flag) to print the block to paste.")

	if failures > 0 {
		return 1
	}
	return 0
}
