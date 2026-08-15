# Writing Providers

A provider teaches mgtt about a technology. You provide three things:

1. **The vocabulary** (`manifest.yaml` + optional `types/*.yaml`) — identity, runtime requirements, install methods, component types, facts, states, failure modes
2. **The binary** — probes real systems, returns typed values
3. **The install script** — builds or downloads the binary (for source installs)

## On this page

- [Manifest structure](#manifest-structure)
- [Directory Structure](#directory-structure)
- [Install](#install)
- [Writing install scripts](#writing-install-scripts)
- [Next Steps](#next-steps) — vocabulary, protocol, testing

## Manifest structure

Every `manifest.yaml` has three top-level blocks:

```yaml
meta:                                # identity
  name: my-provider
  version: 1.0.0
  description: One-line description
  requires:
    mgtt: ">=0.2.0"

runtime:                             # how the provider talks to its backend
  needs: [kubectl]                   # host capabilities at probe time
  network_mode: host                 # optional; bridge (default) | host
  # entrypoint:  optional; convention-default resolves to bin/mgtt-provider-<name>

install:                             # which install methods the provider offers
  source:
    build: hooks/install.sh
    clean: hooks/uninstall.sh
  image:
    repository: ghcr.io/my-org/mgtt-provider-my-provider
```

At least one of `install.source` or `install.image` must be declared.
See [manifest.yaml reference](../reference/manifest.md) for the authoritative schema (fields, invariants, defaults).

## Directory Structure

```
my-provider/
├── manifest.yaml              identity + runtime + install
├── types/                     optional: split component types across files
├── hooks/
│   ├── install.sh             builds the binary (install.source.build)
│   └── uninstall.sh           cleans up build artifacts (install.source.clean)
├── main.go                    implements the protocol
└── bin/
    └── mgtt-provider-my-provider
```

## Install

```bash
# From a local directory
mgtt provider install ./my-provider

# From a git repository
mgtt provider install https://github.com/user/mgtt-provider-redis

# From a Docker image (requires install.image in manifest.yaml)
mgtt provider install --image ghcr.io/user/mgtt-provider-redis:1.0.0@sha256:...
```

`mgtt provider install <name>` picks source when available; `mgtt provider install --image <ref>` forces image. Methods not declared in `install:` are rejected up-front — no deep-in-build failures.

---

## Writing install scripts

`install.source.build` (conventionally `hooks/install.sh`) runs during `mgtt provider install`. Its job: produce the provider binary at `bin/mgtt-provider-<name>`. The matching `install.source.clean` runs during `mgtt provider uninstall <name>` before the provider directory is removed; if it fails, the directory is still removed (uninstall must always succeed).

### For Go providers

```bash
#!/bin/bash
set -e
cd "$(dirname "$0")/.."
mkdir -p bin
go build -o bin/mgtt-provider-my-provider .
echo "built bin/mgtt-provider-my-provider"
```

### For pre-compiled binaries

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
echo "downloaded mgtt-provider-my-provider v${VERSION}"
```

### For Python providers

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
echo "installed Python provider"
```

### Vocabulary-only providers (no binary)

If every fact has an inline `probe.cmd` definition in the manifest, you don't need a binary — the engine shells out directly. A vocabulary-only provider still needs an `install:` block; declare `install.source.build` as a no-op script (or a script that just verifies the required host tools are present). Vocabulary-only providers cannot be installed via `--image` — there's no entrypoint for mgtt to invoke.

---

## Next Steps

- [Vocabulary](vocabulary.md) — writing `types:` / facts / states / failure modes
- [Binary Protocol](protocol.md) — implementing probe/validate/describe
- [Testing](testing.md) — validate, simulate, and live-test your provider
- [manifest.yaml reference](../reference/manifest.md) — full schema
