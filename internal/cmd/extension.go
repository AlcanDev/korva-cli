package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/AlcanDev/korva-cli/internal/extension"
)

// cmdExtension dispatches `korva extension <subcommand>`. Three
// subcommands today: install (download + verify + install), uninstall,
// and status (latest release tag + whether installed). Mirrors the
// inline-switch shape of cmdSkill / cmdPkg.
func cmdExtension(args []string) int {
	if len(args) == 0 {
		extensionUsage(os.Stdout)
		return 0
	}
	switch args[0] {
	case "install":
		return cmdExtensionInstall()
	case "uninstall", "remove", "rm":
		return cmdExtensionUninstall()
	case "status":
		return cmdExtensionStatus()
	case "help", "--help", "-h":
		extensionUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown extension subcommand: %s\n\n", args[0])
		extensionUsage(os.Stderr)
		return 1
	}
}

func extensionUsage(w io.Writer) {
	fmt.Fprint(w, `korva extension — install the sideloaded VS Code companion

Usage:
  korva extension install        Download, verify, and install the latest signed VSIX
  korva extension uninstall      Remove the extension from VS Code
  korva extension status         Show the latest release tag

The extension is sideloaded from a signed GitHub Release at
github.com/AlcanDev/korva-vscode — it is intentionally NOT on the
public VS Code Marketplace. The CLI verifies the ed25519 signature
against an embedded public key before installing, so even a
compromised GitHub release cannot install a tampered .vsix.

After install:
  - Restart VS Code.
  - The status bar shows "Korva: N commands" once the workspace scan
    completes.
  - Use the palette: "Korva: Run command…" to invoke an installed
    Korva package command. Token counts + USD cost flow back into
    your team's dashboard.
`)
}

// cmdExtensionInstall is the happy path the dev runs after `korva
// login`. Resolves the latest release, downloads + verifies, then
// hands off to `code --install-extension`. Each error message points
// at the specific thing the user can fix.
func cmdExtensionInstall() int {
	ctx := context.Background()
	fmt.Println("Resolving latest release at github.com/AlcanDev/korva-vscode…")
	rel, err := extension.LatestRelease(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not fetch latest release: %v\n", err)
		return 1
	}
	fmt.Printf("Found %s (%d bytes). Downloading + verifying…\n", rel.Tag, rel.VsixSize)
	vsixPath, err := extension.DownloadAndVerify(ctx, rel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		fmt.Fprintln(os.Stderr, "  This usually means the release was tampered with — DO NOT install it manually.")
		return 1
	}
	fmt.Println("Signature OK. Installing into VS Code…")
	if err := extension.InstallVSIX(vsixPath); err != nil {
		if errors.Is(err, extension.ErrNoVSCode) {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
		return 1
	}
	fmt.Println("Done. Restart VS Code to activate the Korva extension.")
	fmt.Println("Tip: use the command palette (⇧⌘P) and search 'Korva: Run command…'.")
	return 0
}

func cmdExtensionUninstall() int {
	if err := extension.UninstallExtension(); err != nil {
		if errors.Is(err, extension.ErrNoVSCode) {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		fmt.Fprintf(os.Stderr, "uninstall failed: %v\n", err)
		return 1
	}
	fmt.Println("Korva extension uninstalled (if it was installed).")
	return 0
}

func cmdExtensionStatus() int {
	ctx := context.Background()
	rel, err := extension.LatestRelease(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not fetch latest release: %v\n", err)
		return 1
	}
	fmt.Printf("Latest release: %s\n", rel.Tag)
	fmt.Printf("  %s\n", rel.VsixURL)
	fmt.Printf("  %s\n", rel.SigURL)
	return 0
}
