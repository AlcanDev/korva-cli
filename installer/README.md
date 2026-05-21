# installer

Plug-and-play installation for the Korva CLI.

## Build artifacts

```bash
./installer/build.sh
```

Cross-compiles `korva` for macOS, Linux and Windows (amd64 + arm64) into
`installer/dist/`. On macOS it additionally builds a universal binary and a
`.pkg` installer.

## Install

macOS / Linux:

```bash
./installer/install.sh
```

Windows (PowerShell):

```powershell
.\installer\install.ps1
```

Both scripts detect the OS and architecture, install the matching binary onto
PATH, and print the next steps (`korva login`, `korva setup`).

Editor detection lives in the CLI itself. `korva setup` auto-detects every
supported target (VS Code, Claude Code CLI, Claude Desktop, Cursor, Windsurf)
and writes the right MCP config file for each. Use `--target <name>` to pick
one or several explicitly, `--target all` to force every target, and
`--path <file>` to override the config location for non-standard installs.

Environment overrides: `KORVA_DIST` (artifact directory), `KORVA_PREFIX`
(install location), `KORVA_VERSION` (version stamped into the binary).
