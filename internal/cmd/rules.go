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

Use the Korva MCP without being asked:
- At the start of a non-trivial task (or when entering plan/agent mode), call ` + "`vault_context`" + ` to load recent team knowledge before planning.
- Before risky writes (DB migrations, infra, deploys, deleting data), call ` + "`team_pipeline_state`" + ` and warn if the team is in freeze/incident.
- Before solving a problem or making a design choice, call ` + "`vault_search`" + ` for prior decisions and fixes.
- After a non-trivial decision, fix, or gotcha, call ` + "`vault_save`" + ` (what, why, alternatives). Never save secrets or raw source. This includes requests to "save to memory" / remember something for the team: record it in the Korva vault in parallel with any editor-local memory (local memory is per-machine; the Korva vault is shared with the team).
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

// cmdRules prints the proactive-usage rule block (and where each editor keeps
// its global rules), or installs it into the file-based editors with
// --install. Editors whose global rules aren't a writable file (VS Code,
// Cursor) get printed manual instructions instead.
func cmdRules(args []string) int {
	fs := flag.NewFlagSet("rules", flag.ContinueOnError)
	install := fs.Bool("install", false, "write the rule block into file-based editors' global rules")
	if err := fs.Parse(args); err != nil {
		return 1
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
		fmt.Println("\nRun `korva rules --install` to write it for Claude Code and Windsurf automatically.")
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
