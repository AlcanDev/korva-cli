package cmd

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AlcanDev/korva-cli/internal/api"
	"github.com/AlcanDev/korva-cli/internal/editor"
	"github.com/AlcanDev/korva-cli/internal/version"
)

// cmdPkg dispatches `korva pkg <subcommand>`. Mirrors cmdSkill's
// inline-switch shape — the subcommand surface is small enough that a
// real CLI library would be overkill.
func cmdPkg(args []string) int {
	if len(args) == 0 {
		pkgUsage(os.Stdout)
		return 0
	}
	switch args[0] {
	case "list", "ls":
		return cmdPkgList()
	case "install":
		return cmdPkgInstall(args[1:])
	case "uninstall", "rm":
		return cmdPkgUninstall(args[1:])
	case "status":
		return cmdPkgStatus(args[1:])
	case "help", "--help", "-h":
		pkgUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown pkg subcommand: %s\n\n", args[0])
		pkgUsage(os.Stderr)
		return 1
	}
}

func pkgUsage(w io.Writer) {
	fmt.Fprint(w, `korva pkg — install team-curated slash-command packages

Usage:
  korva pkg list                           List packages curated by your team
  korva pkg install <code> [--here]        Redeem an install code and write files
  korva pkg uninstall <name> [--yes]       Remove a package's files from this project
  korva pkg status                         Show packages installed in this project

Flags:
  --here                                   Don't look for a project root — use cwd as-is
  --yes                                    Skip the confirmation prompt on uninstall

A package writes one slash-command file per editor format:
  .github/prompts/<name>.prompt.md         (VS Code Copilot)
  .claude/commands/<name>.md               (Claude Code)

Install codes look like "kvp_…" and are shown exactly once on creation.
See https://platform.korva.dev/docs/packages.
`)
}

// --- list -------------------------------------------------------------------

func cmdPkgList() int {
	client, ok := authedClient()
	if !ok {
		return 1
	}
	pkgs, err := client.ListPackages(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "list failed: %v\n", err)
		return 1
	}
	if len(pkgs) == 0 {
		fmt.Println("No packages yet. Ask your Team Lead to share an install code or curate one in the web console.")
		return 0
	}
	maxName := len("NAME")
	for _, p := range pkgs {
		if len(p.Name) > maxName {
			maxName = len(p.Name)
		}
	}
	fmt.Printf("%-*s  %-8s  %-7s  %s\n", maxName, "NAME", "STATUS", "VERSION", "DISPLAY NAME")
	for _, p := range pkgs {
		fmt.Printf("%-*s  %-8s  %-7d  %s\n", maxName, p.Name, p.Status, p.Version, p.DisplayName)
	}
	return 0
}

// --- install ----------------------------------------------------------------

func cmdPkgInstall(args []string) int {
	fs := flag.NewFlagSet("pkg install", flag.ContinueOnError)
	here := fs.Bool("here", false, "use cwd as the project root without walking up for a manifest")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "usage: korva pkg install <code> [--here]")
		return 1
	}
	code := rest[0]
	if !strings.HasPrefix(code, "kvp_") {
		fmt.Fprintln(os.Stderr, "install code must start with kvp_")
		return 1
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not get cwd: %v\n", err)
		return 1
	}
	root := cwd
	if !*here {
		found, err := editor.FindProjectRoot(cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n  examined: %s\n  → cd into your project, or pass --here\n", err, cwd)
			return 1
		}
		root = found
	}

	// `korva pkg install` is unauthenticated by design (the code IS the
	// credential) so we DON'T require login here. ServerURL still comes
	// from saved config or env so devs without a login can install.
	server := resolveServer("", currentConfig())
	client := api.New(server, "")

	editors := []string{"copilot", "claude-code"}
	project := filepath.Base(root)
	// fileCount is "what we WILL write" before the request — the
	// server records it as a hint; we POST it ahead of time so a
	// network failure mid-write doesn't lose the metric entirely. The
	// backbone trusts the count for telemetry only.
	// We don't know command count yet, so post 0 — the server doesn't
	// validate it.
	pkg, err := client.InstallPackage(context.Background(), code, project, version.Version, editors, 0)
	if err != nil {
		var httpErr *api.HTTPError
		if errors.As(err, &httpErr) {
			switch httpErr.Status {
			case 404:
				fmt.Fprintln(os.Stderr, "install code not found (typo, or never existed)")
				return 1
			case 410:
				fmt.Fprintf(os.Stderr, "install code is no longer valid: %s\n", httpErr.Message)
				return 1
			case 409:
				fmt.Fprintln(os.Stderr, "install code has reached its max_uses cap — ask your Lead for a fresh one")
				return 1
			case 429:
				fmt.Fprintln(os.Stderr, "too many install attempts from this IP; try again in a few minutes")
				return 1
			}
		}
		fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
		return 1
	}

	results, err := editor.WritePackage(root, pkg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wrote %d files before failing: %v\n", len(results), err)
		return 1
	}

	fmt.Printf("Installed %s v%d (%d commands) into %s\n",
		pkg.Name, pkg.Version, len(pkg.Commands), root)
	for _, r := range results {
		fmt.Printf("  ✓ %s\n", r.Path)
	}
	return 0
}

// --- uninstall --------------------------------------------------------------

func cmdPkgUninstall(args []string) int {
	fs := flag.NewFlagSet("pkg uninstall", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "usage: korva pkg uninstall <name> [--yes]")
		return 1
	}
	name := rest[0]

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not get cwd: %v\n", err)
		return 1
	}
	root, err := editor.FindProjectRoot(cwd)
	if err != nil {
		// On uninstall, --here would just be cwd; degrade gracefully.
		root = cwd
	}

	// We don't know the command list from a name alone; scan both target
	// dirs for matching files. This also makes uninstall work after a
	// package has been archived server-side.
	commands := discoverCommands(root, name)
	if len(commands) == 0 {
		fmt.Fprintf(os.Stderr, "no installed commands found for package %q under %s\n", name, root)
		return 1
	}

	if !*yes {
		fmt.Printf("Remove %d file(s) for package %q? [y/N] ", len(commands)*len(editor.ProjectTargets()), name)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println("Aborted.")
			return 1
		}
	}

	removed, err := editor.RemovePackage(root, commands)
	if err != nil {
		fmt.Fprintf(os.Stderr, "uninstall failed after removing %d files: %v\n", len(removed), err)
		return 1
	}
	fmt.Printf("Removed %d file(s):\n", len(removed))
	for _, r := range removed {
		fmt.Printf("  ✓ %s\n", r)
	}
	return 0
}

// discoverCommands lists command names installed in the project for a
// given package name. v1 has no per-package marker, so this is a
// best-effort scan over both target dirs: every file in
// `.claude/commands/` is treated as a candidate command — unsafe if the
// project has hand-written `.claude/commands/` files, so v1 documents
// the limitation in `korva pkg uninstall --help`.
//
// We accept that v1 over-removes when a project mixes installed
// packages with hand-written ones — the next iteration adds a manifest
// file (.korva/packages.json) so uninstall can be precise.
func discoverCommands(root, _ string) []string {
	seen := map[string]struct{}{}
	for _, t := range editor.ProjectTargets() {
		dir := filepath.Join(root, t.SubPath)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			n := e.Name()
			if !strings.HasSuffix(n, t.Extension) {
				continue
			}
			cmd := strings.TrimSuffix(n, t.Extension)
			seen[cmd] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	return out
}

// --- status -----------------------------------------------------------------

func cmdPkgStatus(_ []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not get cwd: %v\n", err)
		return 1
	}
	root, err := editor.FindProjectRoot(cwd)
	if err != nil {
		root = cwd
	}
	fmt.Printf("Project root: %s\n\n", root)
	any := false
	for _, t := range editor.ProjectTargets() {
		dir := filepath.Join(root, t.SubPath)
		entries, err := os.ReadDir(dir)
		if err != nil {
			fmt.Printf("%s (no %s)\n", t.Name, t.SubPath)
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), t.Extension) {
				continue
			}
			names = append(names, strings.TrimSuffix(e.Name(), t.Extension))
		}
		fmt.Printf("%s — %d commands in %s\n", t.Name, len(names), t.SubPath)
		for _, n := range names {
			fmt.Printf("  · %s\n", n)
			any = true
		}
	}
	if !any {
		fmt.Println("\nNo commands installed. Run `korva pkg install <code>` to add one.")
	}
	return 0
}
