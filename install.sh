#!/bin/bash
set -e

REPO="sajonaro/mgtt"
INSTALL_DIR="/usr/local/bin"

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
esac

# Get latest release tag (fallback to building from source)
VERSION=$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | cut -d'"' -f4 || echo "")

if [ -n "$VERSION" ]; then
  # Try downloading pre-built binary
  URL="https://github.com/${REPO}/releases/download/${VERSION}/mgtt-${OS}-${ARCH}"
  echo "Downloading mgtt ${VERSION} for ${OS}/${ARCH}..."
  if curl -sSfL "$URL" -o /tmp/mgtt 2>/dev/null; then
    chmod +x /tmp/mgtt
    sudo mv /tmp/mgtt "$INSTALL_DIR/mgtt" 2>/dev/null || mv /tmp/mgtt "$INSTALL_DIR/mgtt"
    echo "Installed mgtt ${VERSION} to ${INSTALL_DIR}/mgtt"
    exit 0
  fi
  echo "No pre-built binary for ${OS}/${ARCH}. Falling back to source build."
fi

# Fall back: build from source (requires Go and git)
if ! command -v go &>/dev/null || ! command -v git &>/dev/null; then
  echo "Error: no pre-built binary available and building from source requires Go and git."
  echo ""
  echo "Install Go from https://go.dev/dl/ and git, then run:"
  echo "  git clone https://github.com/${REPO}.git && cd mgtt && go build -o /usr/local/bin/mgtt ./cmd/mgtt"
  exit 1
fi

echo "Building mgtt from source..."
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

git clone --depth=1 "https://github.com/${REPO}.git" "$TMPDIR"
cd "$TMPDIR"
go build -o mgtt ./cmd/mgtt
sudo mv mgtt "$INSTALL_DIR/mgtt" 2>/dev/null || mv mgtt "$INSTALL_DIR/mgtt"
echo "Installed mgtt to ${INSTALL_DIR}/mgtt"
