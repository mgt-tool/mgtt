# Install

## One-liner

```bash
curl -sSL https://raw.githubusercontent.com/sajonaro/mgtt/main/install.sh | sh
```

Downloads a pre-built binary if available, otherwise builds from source via `go install`.

## Go Install

Requires [Go 1.22+](https://go.dev/dl/):

```bash
go install github.com/sajonaro/mgtt/cmd/mgtt@latest
```

## Docker

No installation needed — just Docker:

```bash
docker compose run --rm mgtt version
docker compose run --rm mgtt simulate --all
docker compose run --rm mgtt plan
```

## From Source

```bash
git clone https://github.com/sajonaro/mgtt.git
cd mgtt
go build ./cmd/mgtt
sudo mv mgtt /usr/local/bin/
```

## Install Providers

```bash
# Built-in providers
mgtt provider install kubernetes aws

# Community providers (from GitHub)
mgtt provider install https://github.com/sajonaro/mgtt-provider-docker
```

Verify:

```bash
mgtt provider ls
```
