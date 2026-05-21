// Package cmd implements the Korva CLI commands.
package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/AlcanDev/korva-cli/internal/api"
	"github.com/AlcanDev/korva-cli/internal/config"
	"github.com/AlcanDev/korva-cli/internal/editor"
	"github.com/AlcanDev/korva-cli/internal/version"
)

// Run executes a CLI invocation and returns a process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		usage()
		return 0
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("korva %s\n", version.Version)
		return 0
	case "help", "--help", "-h":
		usage()
		return 0
	case "login":
		return cmdLogin(args[1:])
	case "logout":
		return cmdLogout()
	case "whoami":
		return cmdWhoami()
	case "setup":
		return cmdSetup(args[1:])
	case "status":
		return cmdStatus()
	case "skill", "skills":
		return cmdSkill(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		usage()
		return 1
	}
}

func usage() {
	fmt.Print(`korva — Korva CLI

Usage:
  korva login [--server URL]            Authorize this machine via the browser
  korva logout                          Remove the stored credentials
  korva whoami                          Show the signed-in account
  korva setup [--target T] [--path F]   Wire editors/CLIs to the Korva MCP
  korva status                          Show CLI status + detected targets
  korva skill <subcommand>              Manage team skills (list, show, add, rm)
  korva version                         Print the CLI version

Editor targets:
  vscode | claude-code | claude-desktop | cursor | windsurf | all
  (aliases: claude → claude-code, code → vscode)
`)
}

func cmdLogin(args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	server := fs.String("server", "", "backbone server URL")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, _ := config.Load()
	serverURL := resolveServer(*server, cfg)
	client := api.New(serverURL, "")
	ctx := context.Background()

	start, err := client.StartDeviceLogin(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not start login: %v\n", err)
		return 1
	}

	target := start.VerificationURIComplete
	if target == "" {
		target = start.VerificationURI
	}
	fmt.Printf("\nTo authorize this machine, open:\n  %s\n\nVerification code: %s\n\n",
		target, start.UserCode)
	openBrowser(target)
	fmt.Println("Waiting for approval in the browser...")

	interval := time.Duration(max(start.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(max(start.ExpiresIn, 60)) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		poll, err := client.PollDeviceLogin(ctx, start.DeviceCode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "login failed: %v\n", err)
			return 1
		}
		switch poll.Status {
		case "approved":
			if err := config.Save(config.Config{ServerURL: serverURL, Token: poll.Token}); err != nil {
				fmt.Fprintf(os.Stderr, "could not save credentials: %v\n", err)
				return 1
			}
			fmt.Printf("Logged in as %s.\n", poll.User.Email)
			return 0
		case "expired":
			fmt.Fprintln(os.Stderr, "login code expired; run `korva login` again")
			return 1
		}
	}
	fmt.Fprintln(os.Stderr, "login timed out; run `korva login` again")
	return 1
}

func cmdLogout() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not read config: %v\n", err)
		return 1
	}
	cfg.Token = ""
	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "could not save config: %v\n", err)
		return 1
	}
	fmt.Println("Logged out.")
	return 0
}

func cmdWhoami() int {
	cfg, err := config.Load()
	if err != nil || !cfg.LoggedIn() {
		fmt.Fprintln(os.Stderr, "not logged in; run `korva login`")
		return 1
	}
	user, err := api.New(cfg.ServerURL, cfg.Token).Me(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not reach the server: %v\n", err)
		return 1
	}
	// Role is best-effort: empty when the caller has no team yet.
	roleSuffix := ""
	if user.Role != "" {
		roleSuffix = fmt.Sprintf(" — %s", user.Role)
	}
	fmt.Printf("%s (org %s)%s\n", user.Email, user.OrgID, roleSuffix)
	return 0
}

func cmdStatus() int {
	cfg, _ := config.Load()
	server := cfg.ServerURL
	if server == "" {
		server = config.DefaultServerURL + " (default)"
	}
	fmt.Printf("Server:      %s\n", server)
	fmt.Printf("Logged in:   %v\n", cfg.LoggedIn())
	fmt.Println("Targets:")
	for _, t := range editor.All() {
		mark := "·"
		if t.Detected() {
			mark = "✓"
		}
		fmt.Printf("  %s %-16s %s\n", mark, t.Name, t.DisplayName)
	}
	return 0
}

func cmdSetup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	targetFlag := fs.String("target", "", "comma-separated targets, or \"all\" (default: auto-detect)")
	pathFlag := fs.String("path", "", "override path to the target's MCP config file (requires a single --target)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Load()
	if err != nil || !cfg.LoggedIn() {
		fmt.Fprintln(os.Stderr, "not logged in; run `korva login` first")
		return 1
	}
	if cfg.ServerURL == "" {
		cfg.ServerURL = config.DefaultServerURL
	}

	targets, err := resolveTargets(*targetFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}

	// --path is a manual override; it only makes sense for a single target
	// because each target has its own JSON schema.
	if *pathFlag != "" {
		if len(targets) != 1 {
			fmt.Fprintln(os.Stderr, "--path requires exactly one --target")
			return 1
		}
		if err := writeServerAtPath(*pathFlag, targets[0], cfg.ServerURL, cfg.Token); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", targets[0].DisplayName, err)
			return 1
		}
		fmt.Printf("✓ %s configured (%s)\n", targets[0].DisplayName, *pathFlag)
		return 0
	}

	failures := 0
	configured := 0
	for _, t := range targets {
		path, err := t.ConfigPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %-16s %v\n", t.Name, err)
			failures++
			continue
		}
		if err := t.Configure(cfg.ServerURL, cfg.Token); err != nil {
			fmt.Fprintf(os.Stderr, "✗ %-16s %v\n", t.Name, err)
			failures++
			continue
		}
		configured++
		fmt.Printf("✓ %-16s %s\n", t.Name, path)
	}

	if configured == 0 {
		fmt.Fprintln(os.Stderr, "\nno targets were configured — pass --target <name> to force one")
		return 1
	}
	fmt.Printf("\n%d target(s) configured. Restart them to pick up the \"korva\" MCP server.\n", configured)
	if failures > 0 {
		return 1
	}
	return 0
}

// resolveTargets parses the --target flag. An empty value means "auto-detect
// every installed target on this machine"; "all" forces every registered
// target; a comma-separated list selects specific ones.
func resolveTargets(flagValue string) ([]editor.Target, error) {
	v := strings.TrimSpace(flagValue)
	switch v {
	case "":
		detected := editor.Detected()
		if len(detected) == 0 {
			return nil, fmt.Errorf("no supported editor detected — pass --target <name> (e.g. claude-code) to force one")
		}
		return detected, nil
	case "all":
		return editor.All(), nil
	}

	parts := strings.Split(v, ",")
	out := make([]editor.Target, 0, len(parts))
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		t, err := editor.ByName(name)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--target is empty")
	}
	return out, nil
}

// writeServerAtPath wires the Korva MCP entry into an explicit path using the
// schema of the given target. Used by `korva setup --path` for editors
// installed in non-standard locations.
func writeServerAtPath(path string, t editor.Target, serverURL, token string) error {
	// The Target type owns the schema/key + write logic; we just swap the
	// path. The cleanest way is to clone the target via a manual write.
	return editor.WriteServer(path, t, serverURL, token)
}

func resolveServer(flagValue string, cfg config.Config) string {
	if flagValue != "" {
		return flagValue
	}
	if env := os.Getenv("KORVA_SERVER"); env != "" {
		return env
	}
	if cfg.ServerURL != "" {
		return cfg.ServerURL
	}
	return config.DefaultServerURL
}

func openBrowser(url string) {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	_ = exec.Command(name, args...).Start()
}
