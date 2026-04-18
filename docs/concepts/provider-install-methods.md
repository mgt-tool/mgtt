# Provider Install Methods

Two ways to install a provider — source (git clone and build) or image (pull a Docker image). Both produce the same `~/.mgtt/providers/<name>/` layout; `mgtt plan` doesn't care which method was used.

Which methods are available for a given provider is declared in its `manifest.yaml` under `install:` — `install.source` enables `mgtt provider install <name>`, `install.image` enables `mgtt provider install --image <ref>`. Methods not declared are rejected up-front. See the [manifest.yaml reference](../reference/manifest.md) for the full schema.

## On this page

- [Show me](#show-me) — three concrete examples, side by side
- [Comparison: when to use which](#comparison-when-to-use-which)
- [Digest pinning](#digest-pinning)
- [What gets stored locally](#what-gets-stored-locally)
- [What the image gets at runtime](#what-the-image-gets-at-runtime)
- [Switching install method](#switching-install-method-for-the-same-provider)
- [Registry entries with both](#registry-entries-can-declare-both)

---

## Show me

Three ways to install a provider. Pick the one that fits your workflow.

### 1. Install from the registry (source — default)

Fastest for discovery. The registry lookup happens for you.

```bash
mgtt provider install tempo
```

Looks up `tempo:` in the registry, clones the git repo, builds the binary locally. You need the Go toolchain installed.

### 2. Install from a git URL directly

Full control; no registry lookup. Useful for forked repos, private mirrors, or your own custom providers.

```bash
mgtt provider install https://github.com/mgt-tool/mgtt-provider-tempo
```

Same as above, but you specify the URL directly. Useful when you've forked a provider and want to include your local patches.

### 3. Install from a Docker image

No local build needed. The binary lives in the image; `mgtt` invokes it via `docker run`.

```bash
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-tempo:1.0.0@sha256:abc123...
```

The `@sha256:` digest is required — see [Digest pinning](#digest-pinning).

---

## Comparison: when to use which

| | Source (build from git) | Docker image |
|---|---|---|
| **Declared in manifest** | `install.source.build` | `install.image` |
| **Requires on host** | Go toolchain, git | Docker daemon only |
| **Distribution** | git repo (+ registry for name lookup) | container registry (ghcr, dockerhub, private) |
| **Digest pinning** | commit SHA (if you use it) | image `@sha256:` digest (required) |
| **Review the code** | yes — clone is on disk | no — binary in image |
| **Fork / patch locally** | easy | requires rebuilding the image |
| **Air-gapped / corporate registries** | via private git | via private image registry |
| **Typical user** | provider authors, development | operators, production deployment |

---

## Digest pinning

`mgtt provider install --image` requires the image ref to include `@sha256:<digest>`. Tags can be re-rolled without warning; digests can't.

Find the current digest of a tag from a registry:

```bash
# GHCR, for a tag like :1.0.0
docker buildx imagetools inspect ghcr.io/mgt-tool/mgtt-provider-tempo:1.0.0 \
  --format '{{ .Manifest.Digest }}'
```

Use the returned `sha256:…` in the `--image` ref.

---

## What gets stored locally

Both methods write into `~/.mgtt/providers/<name>/`, but what ends up on disk differs:

**Source install:**
```
~/.mgtt/providers/tempo/
├── .mgtt-install.json     # metadata: method, source URL, timestamp
├── bin/provider           # the compiled executable (built from source)
└── <maybe other files>    # manifest.yaml, docs, examples, etc.
```

**Image install:**
```
~/.mgtt/providers/tempo/
├── .mgtt-install.json     # metadata: method, image digest, timestamp
└── manifest.yaml          # provider descriptor only; binary lives in the Docker image
```

The `.mgtt-install.json` file records:

```json
{
  "method": "image",
  "source": "ghcr.io/mgt-tool/mgtt-provider-tempo:1.0.0@sha256:abcdef...",
  "installed_at": "2026-04-17T10:30:00Z",
  "version": "1.0.0"
}
```

Or for source:

```json
{
  "method": "source",
  "source": "https://github.com/mgt-tool/mgtt-provider-tempo",
  "installed_at": "2026-04-17T10:30:00Z",
  "version": "1.0.0"
}
```

The `mgtt provider list` command surfaces this:

```bash
$ mgtt provider list
  tempo    v1.0.0    source  Per-span SLO checks against Grafana Tempo
  quickwit v1.0.0    image   Cross-span tracing checks against Quickwit
```

---

## What the image gets at runtime

Image-installed providers run via `docker run`. The container doesn't inherit your shell by default — mgtt injects bind mounts and env forwards based on what the provider declared in `manifest.yaml` under `runtime:`:

```yaml
runtime:
  needs: [kubectl, aws]
  network_mode: host
```

- `runtime.needs:` — named capabilities (host tools, credential chains, sockets). Built-in labels: `kubectl`, `aws`, `docker`, `terraform`, `gcloud`, `azure`.
- `runtime.network_mode:` — container network mode. `bridge` (default) or `host`.

Both fields show up in `mgtt provider install --image` output, in `mgtt provider ls`, and in the [public registry](../reference/registry.md).

Full vocabulary, operator overrides (`$MGTT_HOME/capabilities.yaml`), the `MGTT_IMAGE_CAP_<NAME>` env shortcut, and the `MGTT_IMAGE_CAPS_DENY` opt-out live in [Provider Capabilities](../reference/image-capabilities.md). For the full operator handbook — including auditing installed providers and troubleshooting capability forwards — see [Using Providers](./using-providers.md).

---

## Switching install method for the same provider

Both methods use the same local directory structure, so switching is straightforward:

1. **Uninstall** the old method:
   ```bash
   mgtt provider uninstall tempo
   ```

2. **Reinstall** using the new method:
   ```bash
   mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-tempo:1.0.0@sha256:abc123...
   ```

Uninstalling from an image prints a `docker rmi` hint for cleanup:

```
Uninstalled tempo (image method)
To clean up the image:
  docker rmi ghcr.io/mgt-tool/mgtt-provider-tempo:1.0.0@sha256:abc123...
```

Optional — the image won't interfere with future installs.

---

## Registry entries can declare both

The public registry (`docs/registry.yaml`) supports an optional `image:` field alongside `url:`. Either field can seed `mgtt provider install <name>`:

```yaml
tempo:
  url: https://github.com/mgt-tool/mgtt-provider-tempo
  image: ghcr.io/mgt-tool/mgtt-provider-tempo:1.0.0@sha256:abc123...
  description: Per-span SLO checks against Grafana Tempo
  tags:
    - tracing
    - otel
```

When you run `mgtt provider install tempo`, it uses the git URL by default (if the provider's manifest declares `install.source`). The `image:` field in the registry is a placeholder for future enhancements — today `--image` requires an explicit, fully-qualified image ref with a digest:

```bash
mgtt provider install tempo           # source install (requires install.source in the manifest)
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-tempo:1.0.0@sha256:abc123...   # image install (requires install.image in the manifest)
```

---

## See also

- [Multi-File Models](./multi-file-models.md) — the other provider methodology doc: when and how to split a system into several model files
