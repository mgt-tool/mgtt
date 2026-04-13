#!/bin/bash
set -e

echo "Uninstalling mgtt..."

# Remove binary
if [ -f /usr/local/bin/mgtt ]; then
  sudo rm -f /usr/local/bin/mgtt 2>/dev/null || rm -f /usr/local/bin/mgtt
  echo "  removed /usr/local/bin/mgtt"
elif command -v mgtt &>/dev/null; then
  MGTT_PATH=$(command -v mgtt)
  sudo rm -f "$MGTT_PATH" 2>/dev/null || rm -f "$MGTT_PATH"
  echo "  removed $MGTT_PATH"
else
  echo "  mgtt binary not found, skipping"
fi

# Remove data directory (providers, cache)
if [ -d "$HOME/.mgtt" ]; then
  rm -rf "$HOME/.mgtt"
  echo "  removed ~/.mgtt"
else
  echo "  ~/.mgtt not found, skipping"
fi

echo "mgtt uninstalled."
