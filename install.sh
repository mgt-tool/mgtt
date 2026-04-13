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

# Fall back: build from source (requires Go)
if ! command -v go &>/dev/null; then
  echo "Error: no pre-built binary available and Go is not installed."
  echo ""
  echo "Install Go from https://go.dev/dl/ then run:"
  echo "  go install github.com/${REPO}/cmd/mgtt@latest"
  exit 1
fi

echo "Building mgtt from source..."
GOPROXY=direct go install "github.com/${REPO}/cmd/mgtt@latest"

GOBIN="$(go env GOPATH)/bin/mgtt"
if [ -f "$GOBIN" ]; then
  sudo mv "$GOBIN" "$INSTALL_DIR/mgtt" 2>/dev/null || mv "$GOBIN" "$INSTALL_DIR/mgtt"
  echo "Installed mgtt to ${INSTALL_DIR}/mgtt"
else
  echo "Installed mgtt to $(go env GOPATH)/bin/mgtt"
  echo "Note: make sure $(go env GOPATH)/bin is on your PATH"
fi
