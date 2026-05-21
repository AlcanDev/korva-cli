# korva

Command-line client for the [Korva Platform](https://korva.dev) — connects
AI coding assistants (Claude Code, Claude Desktop, Cursor, Windsurf, VS Code)
to your team's MCP server so they share memory, governed skills and team
standards.

This repository contains only the open-source CLI. The Korva backbone is
a separate, proprietary service.

## Install

### Homebrew (macOS, Linux)

```bash
brew install alcandev/tap/korva
```

### One-liner (macOS, Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/AlcanDev/korva-cli/main/installer/install.sh | sh
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/AlcanDev/korva-cli/main/installer/install.ps1 | iex
```

### Manual

Grab the binary for your platform from the
[latest release](https://github.com/AlcanDev/korva-cli/releases/latest) and
drop it on your `PATH`.

### From source

```bash
git clone https://github.com/AlcanDev/korva-cli.git
cd korva-cli
make build       # binary in ./bin/korva
make installer   # all cross-platform binaries + macOS .pkg in installer/dist/
```

Requires Go 1.26+.

## Quickstart

```bash
# 1. Authorize this machine via the browser (email + OTP)
korva login --server https://korva.your-team.com

# 2. Wire every detected editor/CLI to the team's MCP server
korva setup

# 3. Inspect status and team skills
korva status
korva skill list
```

After `korva setup`, restart your editors — the `korva` MCP server appears
with your team's approved skills plus the built-in vault tools
(`vault_save`, `vault_search`, `vault_context`).

## Supported editors

`korva setup` auto-detects every installed target and writes the right
config file in the right schema.

| Target          | `--target` name   | Config file                                                   |
|-----------------|-------------------|---------------------------------------------------------------|
| VS Code         | `vscode`          | `~/Library/Application Support/Code/User/mcp.json` (mac), `~/.config/Code/User/mcp.json` (linux), `%APPDATA%\Code\User\mcp.json` (win) |
| Claude Code CLI | `claude-code`     | `~/.claude.json`                                              |
| Claude Desktop  | `claude-desktop`  | `~/Library/Application Support/Claude/claude_desktop_config.json` (mac), `%APPDATA%\Claude\claude_desktop_config.json` (win) |
| Cursor          | `cursor`          | `~/.cursor/mcp.json`                                          |
| Windsurf        | `windsurf`        | `~/.codeium/windsurf/mcp_config.json`                         |

Aliases: `claude` → `claude-code`, `code` → `vscode`.

```bash
# Install only on specific targets
korva setup --target claude,cursor

# Force-install on every supported target (even if not detected)
korva setup --target all

# Override path for non-standard installs
korva setup --target vscode --path /path/to/Code/User/mcp.json
```

## Commands

| Command                              | What it does                                                 |
|--------------------------------------|--------------------------------------------------------------|
| `korva login [--server URL]`         | Browser-based device-flow login. Saves a token locally.      |
| `korva logout`                       | Forget the stored token.                                     |
| `korva whoami`                       | Show signed-in account + org + role.                         |
| `korva setup [--target T] [--path F]`| Wire one or more editors to the team's MCP server.           |
| `korva status`                       | Show server URL, login state, and detected editor targets.   |
| `korva skill list`                   | List the team's skills with status.                          |
| `korva skill show <name>`            | Show one skill (body, inputs, status).                       |
| `korva skill add <name> ...`         | Create or update a skill (Team Lead).                        |
| `korva skill rm <name>`              | Delete a skill (Team Lead).                                  |
| `korva skill propose <name> ...`     | Propose a skill for review.                                  |
| `korva skill approve <name>`         | Approve a pending skill (Team Lead).                         |
| `korva skill reject <name>`          | Reject a pending skill (Team Lead).                          |
| `korva version`                      | Print the CLI version.                                       |

### Environment variables

| Variable          | Effect                                                                  |
|-------------------|-------------------------------------------------------------------------|
| `KORVA_SERVER`    | Backbone URL used by every command (overrides config, beaten by flags). |
| `KORVA_HOME`      | Directory holding `config.json` (default: `~/.korva`, `%APPDATA%\korva` on Windows). |
| `KORVA_DIST`      | Where `install.sh` / `install.ps1` look for binaries.                   |
| `KORVA_PREFIX`    | Where the install scripts copy `korva` to.                              |
| `KORVA_VERSION`   | Stamped into the binary at build time (used by `installer/build.sh`).   |

## Releases

Every push to `main` that passes CI produces a new patch-bumped GitHub
Release with binaries for macOS (Intel + Apple Silicon, plus a universal
`.pkg`), Linux (amd64/arm64) and Windows (amd64/arm64), plus a
`SHA256SUMS` checksum file. The release workflow also opens a PR to
[AlcanDev/homebrew-tap](https://github.com/AlcanDev/homebrew-tap) to bump
the Homebrew formula.

To release a specific version, dispatch
[`release.yml`](.github/workflows/release.yml) manually with the
`version` input set (e.g. `v0.3.0`).

## Contributing

Issues and pull requests welcome. Run `make test` before submitting.

## License

[MIT](LICENSE) — see the `LICENSE` file for the full text.
