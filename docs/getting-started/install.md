# Install

## On this page

- [One-liner](#one-liner) — fastest path
- [Go Install](#go-install)
- [Docker](#docker)
- [From Source](#from-source)
- [Uninstall](#uninstall)
- [Install Providers](#install-providers)

---

## One-liner

```bash
curl -sSL https://raw.githubusercontent.com/mgt-tool/mgtt/main/install.sh | sh
```

Downloads a pre-built binary if available, otherwise builds from source via `go install`.

## Go Install

Requires [Go 1.22+](https://go.dev/dl/):

```bash
go install github.com/mgt-tool/mgtt/cmd/mgtt@latest
```

## Docker

No installation needed — just Docker. Mount your project directory as `/workspace`:

```bash
docker run --rm -v $(pwd):/workspace ghcr.io/mgt-tool/mgtt version
docker run --rm -v $(pwd):/workspace ghcr.io/mgt-tool/mgtt simulate --all
docker run --rm -v $(pwd):/workspace ghcr.io/mgt-tool/mgtt model validate
```

For live troubleshooting, also mount your credentials:

```bash
docker run --rm \
  -v $(pwd):/workspace \
  -v ~/.kube:/home/mgtt/.kube:ro \
  -v ~/.aws:/home/mgtt/.aws:ro \
  ghcr.io/mgt-tool/mgtt plan
```

## From Source

```bash
git clone https://github.com/mgt-tool/mgtt.git
cd mgtt
go build ./cmd/mgtt
sudo mv mgtt /usr/local/bin/
```

## Uninstall

```bash
curl -sSL https://raw.githubusercontent.com/mgt-tool/mgtt/main/uninstall.sh | sh
```

Removes the `mgtt` binary and the `~/.mgtt` data directory (installed providers and cache).

## Install Providers

```bash
# Official providers (resolved via registry)
mgtt provider install kubernetes aws

# Community providers (from GitHub URL)
mgtt provider install https://github.com/mgt-tool/mgtt-provider-docker
```

Verify:

```bash
mgtt provider ls
```

### Providers in Docker

Providers are installed at runtime, not baked into the image. Use a named volume to persist them across runs:

```bash
# Install providers (persisted in the mgtt-data volume)
docker run --rm -v mgtt-data:/data ghcr.io/mgt-tool/mgtt provider install kubernetes aws

# Run commands — mount both your project and the provider volume
docker run --rm \
  -v $(pwd):/workspace \
  -v mgtt-data:/data \
  ghcr.io/mgt-tool/mgtt simulate --all
```

The `mgtt-data` volume stores installed providers and the registry cache. Install once, reuse across runs.

For live troubleshooting, add your credentials:

```bash
docker run --rm \
  -v $(pwd):/workspace \
  -v mgtt-data:/data \
  -v ~/.kube:/home/mgtt/.kube:ro \
  -v ~/.aws:/home/mgtt/.aws:ro \
  ghcr.io/mgt-tool/mgtt plan
```

!!! tip "Shell alias"
    To avoid typing the volume mounts every time:

    ```bash
    alias mgtt='docker run --rm -v $(pwd):/workspace -v mgtt-data:/data ghcr.io/mgt-tool/mgtt'
    mgtt simulate --all
    mgtt provider ls
    ```
