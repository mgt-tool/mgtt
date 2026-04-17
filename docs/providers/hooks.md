# Install Hooks

`hooks/install.sh` runs during `mgtt provider install`. Its job: produce the provider binary.

## On this page

- [For Go providers](#for-go-providers)
- [For pre-compiled binaries](#for-pre-compiled-binaries) — download & install
- [For Python providers](#for-python-providers) — venv + wrapper
- [Vocabulary-only providers (no hook)](#vocabulary-only-providers-no-hook)
- [Next steps](#next-steps)

---

## For Go providers

```bash
#!/bin/bash
set -e
cd "$(dirname "$0")/.."
mkdir -p bin
go build -o bin/mgtt-provider-my-provider .
echo "✓ built bin/mgtt-provider-my-provider"
```

## For pre-compiled binaries

```bash
#!/bin/bash
set -e
cd "$(dirname "$0")/.."
mkdir -p bin

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in x86_64) ARCH="amd64" ;; aarch64) ARCH="arm64" ;; esac

VERSION=$(grep 'version:' manifest.yaml | head -1 | awk '{print $2}')
URL="https://github.com/my-org/mgtt-provider-my-provider/releases/download/v${VERSION}/mgtt-provider-my-provider-${OS}-${ARCH}"

curl -sSL "$URL" -o bin/mgtt-provider-my-provider
chmod +x bin/mgtt-provider-my-provider
echo "✓ downloaded mgtt-provider-my-provider v${VERSION}"
```

## For Python providers

```bash
#!/bin/bash
set -e
cd "$(dirname "$0")/.."
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt
mkdir -p bin
cat > bin/mgtt-provider-my-provider <<'WRAPPER'
#!/bin/bash
exec "$(dirname "$0")/../.venv/bin/python" "$(dirname "$0")/../main.py" "$@"
WRAPPER
chmod +x bin/mgtt-provider-my-provider
echo "✓ installed Python provider"
```

## Vocabulary-only providers (no hook)

If all your facts have inline `probe.cmd` definitions in `manifest.yaml`, you don't need a binary or install hook. Set `meta.command: ""` and `hooks.install: ""`. This is the quick-start path for prototyping.

---

## Next steps

- [Vocabulary](vocabulary.md) — writing manifest.yaml
- [Binary Protocol](protocol.md) — implementing the probe commands
- [Testing](testing.md) — validate, simulate, and live-test your provider
