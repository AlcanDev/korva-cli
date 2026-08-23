package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlcanDev/korva-cli/internal/api"
	"github.com/AlcanDev/korva-cli/internal/contextfiles"
	"github.com/AlcanDev/korva-cli/internal/editor"
	"github.com/AlcanDev/korva-cli/internal/gitinfo"
)

// pushTokenEnv is where CI provides the project-scoped push token.
const pushTokenEnv = "KORVA_CONTEXT_TOKEN"

// cmdContext dispatches `korva context <subcommand>`. Same inline-switch
// shape as cmdPkg / cmdSkill.
func cmdContext(args []string) int {
	if len(args) == 0 {
		contextUsage(os.Stdout)
		return 0
	}
	switch args[0] {
	case "push":
		return cmdContextPush(args[1:])
	case "pull":
		return cmdContextPull(args[1:])
	case "help", "--help", "-h":
		contextUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown context subcommand: %s\n\n", args[0])
		contextUsage(os.Stderr)
		return 1
	}
}

func contextUsage(w io.Writer) {
	fmt.Fprint(w, `korva context — the living per-project source of truth

Usage:
  korva context push [--project N] [--server URL] [--here]
      Report this repo's default-branch state to Korva. Meant for CI on
      merges to main. Auth: the KORVA_CONTEXT_TOKEN env var, a
      project-scoped kctx_… token minted in the Korva console
      (Projects → your project → Push tokens). Never use a personal
      API token here.

  korva context pull [--project N] [--here] [--no-gitignore]
      Write .korva/context/{portfolio,project-brief,decisions}.md —
      plain markdown any AI agent reads for portfolio awareness.
      Requires korva login. Adds .korva/ to .gitignore unless told not
      to.

Flags:
  --project N        Override the project name (default: project root folder name)
  --server URL       Backbone URL (default: $KORVA_SERVER, then the login config)
  --here             Use cwd as the project root without walking up
  --no-gitignore     Don't touch .gitignore on pull

See https://platform.korva.dev/docs/projects.
`)
}

// normalizeProject mirrors the backbone's NormalizeProject so the CLI
// derives the same canonical name the server stores.
func normalizeProject(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	if p == "" {
		return ""
	}
	return strings.Join(strings.Fields(p), "-")
}

// resolveRoot applies the shared --here / project-root convention.
func resolveRoot(here bool) (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not get cwd: %v\n", err)
		return "", false
	}
	if here {
		return cwd, true
	}
	root, err := editor.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n  examined: %s\n  → cd into your project, or pass --here\n", err, cwd)
		return "", false
	}
	return root, true
}

// --- push -------------------------------------------------------------------

func cmdContextPush(args []string) int {
	fs := flag.NewFlagSet("context push", flag.ContinueOnError)
	project := fs.String("project", "", "project name (default: root folder name)")
	server := fs.String("server", "", "backbone URL")
	here := fs.Bool("here", false, "use cwd as the project root")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	token := os.Getenv(pushTokenEnv)
	if token == "" {
		fmt.Fprintf(os.Stderr, "%s is not set.\n  Mint a push token in the Korva console (Projects → your project → Push tokens)\n  and add it as a CI secret.\n", pushTokenEnv)
		return 1
	}
	if !strings.HasPrefix(token, "kctx_") {
		fmt.Fprintf(os.Stderr, "%s must be a kctx_… push token (got a different credential).\n  Personal API tokens are rejected by design — mint a project-scoped token instead.\n", pushTokenEnv)
		return 1
	}

	root, ok := resolveRoot(*here)
	if !ok {
		return 1
	}
	name := normalizeProject(*project)
	if name == "" {
		name = normalizeProject(filepath.Base(root))
	}

	info, err := gitinfo.Collect(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read git state: %v\n", err)
		return 1
	}

	client := api.New(resolveServer(*server, currentConfig()), token)
	pc, err := client.PushContext(context.Background(), api.ContextPush{
		Project:     name,
		HeadSHA:     info.HeadSHA,
		Branch:      info.Branch,
		CommittedAt: info.CommittedAt,
		RepoURL:     info.RepoURL,
		Branches:    info.Branches,
	})
	if err != nil {
		var httpErr *api.HTTPError
		if errors.As(err, &httpErr) && httpErr.Status == 401 {
			fmt.Fprintf(os.Stderr, "push rejected (401): %s\n  → the token was revoked or never existed; mint a new one in the console.\n", httpErr.Message)
			return 1
		}
		fmt.Fprintf(os.Stderr, "push failed: %v\n", err)
		return 1
	}

	short := pc.HeadSHA
	if len(short) > 12 {
		short = short[:12]
	}
	fmt.Printf("context pushed: %s → %s@%s\n", pc.Project, pc.Branch, short)
	if pc.Stale {
		fmt.Println("note: the project brief is now STALE — a Lead should refresh it in the console.")
	}
	return 0
}

// --- pull -------------------------------------------------------------------

func cmdContextPull(args []string) int {
	fs := flag.NewFlagSet("context pull", flag.ContinueOnError)
	project := fs.String("project", "", "project name (default: root folder name)")
	here := fs.Bool("here", false, "use cwd as the project root")
	noGitignore := fs.Bool("no-gitignore", false, "don't touch .gitignore")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	client, ok := authedClient()
	if !ok {
		return 1
	}
	root, ok := resolveRoot(*here)
	if !ok {
		return 1
	}
	name := normalizeProject(*project)
	if name == "" {
		name = normalizeProject(filepath.Base(root))
	}

	ctx := context.Background()
	data := contextfiles.Data{
		ServerURL:   client.ServerURL,
		Project:     name,
		GeneratedAt: time.Now(),
	}

	pc, err := client.GetProjectContext(ctx, name)
	if err == nil {
		data.Context = &pc
	} else {
		var httpErr *api.HTTPError
		if !errors.As(err, &httpErr) || httpErr.Status != 404 {
			fmt.Fprintf(os.Stderr, "load project context: %v\n", err)
			return 1
		}
		// 404 = no record yet: the brief file explains how to wire it.
	}

	if data.Portfolio, err = client.Portfolio(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "load portfolio: %v\n", err)
		return 1
	}
	if data.Observations, err = client.ListObservations(ctx, name, 20); err != nil {
		fmt.Fprintf(os.Stderr, "load vault entries: %v\n", err)
		return 1
	}

	written, err := contextfiles.Write(root, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write context files: %v\n", err)
		return 1
	}
	for _, rel := range written {
		fmt.Printf("wrote %s\n", rel)
	}

	if !*noGitignore {
		changed, err := contextfiles.EnsureGitignore(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "update .gitignore: %v\n", err)
			return 1
		}
		if changed {
			fmt.Println("added .korva/ to .gitignore")
		}
	}
	if data.Context == nil {
		fmt.Println("note: this project has no context record yet — see .korva/context/project-brief.md for how to wire CI.")
	}
	return 0
}
