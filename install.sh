#!/bin/sh
# Copyright (C) 2026 Alex Kunich
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Install mgtt.
#
#   curl -sSLO https://raw.githubusercontent.com/mgt-tool/mgtt/main/install.sh
#   sh install.sh                              the newest release
#   MGTT_VERSION=v0.3.0 sh install.sh          a pinned one
#   INSTALL_DIR=$HOME/.local/bin sh install.sh
#
# A release ships one binary per platform and a SHA256SUMS over them; this
# downloads the one for this machine and checks it against the sums before
# it is installed.  Where there is no binary for the platform, the fallback
# is `go install` at the same version -- still pinned, never "whatever main
# is today".
set -eu

REPO="mgt-tool/mgtt"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
VERSION="${MGTT_VERSION:-}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64)        arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
esac

if [ -z "$VERSION" ]; then
  VERSION=$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1 || true)
fi
if [ -z "$VERSION" ]; then
  echo "install: no release found; set MGTT_VERSION=vX.Y.Z to pin one" >&2
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

place() {
  chmod +x "$1"
  if [ -w "$INSTALL_DIR" ]; then mv "$1" "$INSTALL_DIR/mgtt"; else sudo mv "$1" "$INSTALL_DIR/mgtt"; fi
  echo "installed mgtt $VERSION to $INSTALL_DIR/mgtt"
  "$INSTALL_DIR/mgtt" version
}

base="https://github.com/${REPO}/releases/download/${VERSION}"
asset="mgtt-${os}-${arch}"
echo "mgtt $VERSION for $os/$arch"
if curl -sSfL "$base/$asset" -o "$tmp/$asset" 2>/dev/null \
   && curl -sSfL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS" 2>/dev/null; then
  want=$(grep " $asset\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)
  if command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
  else got=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1); fi
  [ -n "$want" ] && [ "$want" = "$got" ] || {
    echo "install: $asset does not match SHA256SUMS; refusing to install it" >&2; exit 1; }
  place "$tmp/$asset"
  exit 0
fi

echo "no binary for $os/$arch in $VERSION; building it with go install"
if ! command -v go >/dev/null 2>&1; then
  echo "install: that needs Go -- https://go.dev/dl/ -- then:" >&2
  echo "  go install github.com/${REPO}/cmd/mgtt@${VERSION}" >&2
  exit 1
fi
GOBIN="$tmp" go install "github.com/${REPO}/cmd/mgtt@${VERSION}"
place "$tmp/mgtt"
