#!/usr/bin/env bash
# build.sh — cross-compiles the korva CLI for every supported platform and,
# on macOS, builds a universal binary and a .pkg installer.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/installer/dist"
VERSION="${KORVA_VERSION:-0.0.0-dev}"
PKG="github.com/AlcanDev/korva-cli/cmd/korva"
LDFLAGS="-s -w -X github.com/AlcanDev/korva-cli/internal/version.Version=${VERSION}"

rm -rf "$DIST"
mkdir -p "$DIST"

targets=(darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64)
for target in "${targets[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  out="korva-${os}-${arch}"
  [ "$os" = "windows" ] && out="${out}.exe"
  echo "building ${out}"
  ( cd "$ROOT" && GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 \
      go build -trimpath -ldflags "$LDFLAGS" -o "$DIST/$out" "$PKG" )
done

# macOS: universal binary + .pkg installer.
if [ "$(uname -s)" = "Darwin" ] && command -v lipo >/dev/null 2>&1; then
  echo "building macOS universal binary + .pkg"
  lipo -create "$DIST/korva-darwin-amd64" "$DIST/korva-darwin-arm64" \
    -output "$DIST/korva-darwin-universal"

  pkgroot="$(mktemp -d)"
  mkdir -p "$pkgroot/usr/local/bin"
  install -m 0755 "$DIST/korva-darwin-universal" "$pkgroot/usr/local/bin/korva"
  pkgbuild --root "$pkgroot" \
    --identifier dev.korva.cli \
    --version "$VERSION" \
    --install-location / \
    "$DIST/korva-${VERSION}-macos.pkg"
  rm -rf "$pkgroot"
fi

echo
echo "artifacts in $DIST:"
ls -1 "$DIST"
