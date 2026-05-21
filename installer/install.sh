#!/usr/bin/env bash
# install.sh — plug-and-play installer for the korva CLI on macOS and Linux.
# Detects the OS/architecture and installs the matching binary onto PATH.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST="${KORVA_DIST:-$SCRIPT_DIR/dist}"
PREFIX="${KORVA_PREFIX:-/usr/local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux|darwin) ;;
  *) echo "error: unsupported OS '$os'" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "error: unsupported architecture '$arch'" >&2; exit 1 ;;
esac

binary="$DIST/korva-${os}-${arch}"
if [ ! -f "$binary" ]; then
  echo "error: binary not found: $binary" >&2
  echo "       run installer/build.sh first, or set KORVA_DIST." >&2
  exit 1
fi

if [ ! -d "$PREFIX" ]; then
  mkdir -p "$PREFIX" 2>/dev/null || {
    echo "error: cannot create $PREFIX (try: sudo, or set KORVA_PREFIX)" >&2
    exit 1
  }
fi

install -m 0755 "$binary" "$PREFIX/korva"
echo "Installed korva to $PREFIX/korva"
"$PREFIX/korva" version

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *) echo; echo "note: $PREFIX is not on your PATH — add it to your shell profile." ;;
esac

echo
echo "Next steps:"
echo "  korva login    # authorize this machine"
echo "  korva setup    # wire VS Code, Claude Code, Cursor, Windsurf to Korva"
echo "  korva status   # see which editors were detected"
