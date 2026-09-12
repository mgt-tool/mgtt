# Install

Every release of mgtt is published three ways. Each one can be pinned to a
version, and `mgtt version` answers the same `vX.Y.Z` whichever way it came.

- [The installer](#the-installer) — a checksummed binary from the GitHub release
- [Go install](#go-install) — from source, through the Go module proxy
- [Docker](#docker) — an image on ghcr.io, `linux/amd64` and `linux/arm64`
- [The SDK, as a library](#the-sdk-as-a-library)

Releases are listed at <https://github.com/mgt-tool/mgtt/releases>; each is a
section of [CHANGELOG.md](https://github.com/mgt-tool/mgtt/blob/main/CHANGELOG.md).

## The installer

```sh
curl -sSLO https://raw.githubusercontent.com/mgt-tool/mgtt/main/install.sh
sh install.sh                                   # the newest release
MGTT_VERSION=v0.3.0 sh install.sh               # a pinned one
INSTALL_DIR=$HOME/.local/bin sh install.sh      # somewhere other than /usr/local/bin
```

It downloads `mgtt-<os>-<arch>` for this machine and checks it against the
release's `SHA256SUMS` before installing it. Where a release has no binary for
the platform, it falls back to `go install` **at the same version** — a
fallback is never "whatever main is today".

## Go install

```sh
go install github.com/mgt-tool/mgtt/cmd/mgtt@v0.3.0     # pinned
go install github.com/mgt-tool/mgtt/cmd/mgtt@latest     # the newest release
```

Needs Go 1.25 or newer. The binary reads its version from the module
information Go stamps into it, so `mgtt version` reports the tag you asked for.

## Docker

```sh
docker run --rm -v "$PWD:/workspace" ghcr.io/mgt-tool/mgtt:0.3.0 version
docker run --rm -v "$PWD:/workspace" ghcr.io/mgt-tool/mgtt:0.3.0 simulate --all
docker run --rm -v "$PWD:/workspace" ghcr.io/mgt-tool/mgtt:0.3.0 model validate
```

Tags, and what moves them:

| tag | is | moves when |
|---|---|---|
| `0.3.0` | that release | never |
| `0.3`, `0` | the newest release in that line | a release in the line |
| `latest` | the newest release | a release |
| `edge`, `sha-<commit>` | a build of main | every push to main |

In CI, pin `X.Y.Z`. `latest` is for a laptop.

## The SDK, as a library

Provider runners are built against `sdk/provider`, which is versioned with
the rest of the module:

```sh
go get github.com/mgt-tool/mgtt/sdk/provider@v0.3.0
```

The `internal/` packages are not importable, by Go's rule; what a provider
needs is under `sdk/`.
