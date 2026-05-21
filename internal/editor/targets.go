package editor

import (
	"errors"
	"path/filepath"
)

// registry holds every target Korva knows how to wire up. The schemaKey
// follows the upstream tool: VS Code uses "servers" in mcp.json, every other
// supported tool uses "mcpServers".
var registry = map[string]Target{
	"vscode": {
		Name:        "vscode",
		DisplayName: "VS Code",
		schemaKey:   "servers",
		pathFn:      vscodePath,
	},
	"claude-code": {
		Name:        "claude-code",
		DisplayName: "Claude Code (CLI)",
		schemaKey:   "mcpServers",
		pathFn:      claudeCodePath,
	},
	"claude-desktop": {
		Name:        "claude-desktop",
		DisplayName: "Claude Desktop",
		schemaKey:   "mcpServers",
		pathFn:      claudeDesktopPath,
	},
	"cursor": {
		Name:        "cursor",
		DisplayName: "Cursor",
		schemaKey:   "mcpServers",
		pathFn:      cursorPath,
	},
	"windsurf": {
		Name:        "windsurf",
		DisplayName: "Windsurf",
		schemaKey:   "mcpServers",
		pathFn:      windsurfPath,
	},
}

// aliases let the user type shorter / more natural names on the CLI.
var aliases = map[string]string{
	"claude":  "claude-code",
	"code":    "vscode",
	"vs-code": "vscode",
}

// ─── path resolvers ────────────────────────────────────────────────────────
//
// Each pathFn returns the canonical location of the target's MCP config file
// for the current OS. They route through pathFor* helpers that take the GOOS
// and env vars as inputs so they're directly unit-testable for every platform.

func vscodePath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return vscodePathFor(currentGOOS(), home, envOrEmpty("APPDATA"))
}

func vscodePathFor(goos, home, appData string) (string, error) {
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json"), nil
	case "windows":
		if appData == "" {
			return "", errors.New("APPDATA is not set")
		}
		return filepath.Join(appData, "Code", "User", "mcp.json"), nil
	default:
		return filepath.Join(home, ".config", "Code", "User", "mcp.json"), nil
	}
}

// claudeCodePath returns the user-level config for the Claude Code CLI.
// Claude Code keeps user config in ~/.claude.json (which contains far more
// than MCP servers — preserving siblings keys is critical).
func claudeCodePath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude.json"), nil
}

// claudeDesktopPath returns the Claude Desktop config path.
//
// Reference: docs.anthropic.com/en/docs/claude-code/mcp says Claude Desktop
// reads `claude_desktop_config.json` from a per-OS location. Linux is not
// officially supported by the desktop app — we map to ~/.config/Claude/ as
// a best-effort for users who run it via Wine/community builds.
func claudeDesktopPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return claudeDesktopPathFor(currentGOOS(), home, envOrEmpty("APPDATA"))
}

func claudeDesktopPathFor(goos, home, appData string) (string, error) {
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), nil
	case "windows":
		if appData == "" {
			return "", errors.New("APPDATA is not set")
		}
		return filepath.Join(appData, "Claude", "claude_desktop_config.json"), nil
	default:
		return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json"), nil
	}
}

// cursorPath returns Cursor's user-level mcp.json. Cursor uses the same path
// (~/.cursor/mcp.json) on every supported OS.
func cursorPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor", "mcp.json"), nil
}

// windsurfPath returns Windsurf's user-level mcp_config.json. Windsurf lives
// under Codeium's directory regardless of OS.
func windsurfPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"), nil
}
