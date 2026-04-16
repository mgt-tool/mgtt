# Provider Install Methods

Two ways to install a provider — git clone and build, or pull a Docker image. Both produce the same `~/.mgtt/providers/<name>/` layout; `mgtt plan` doesn't care which method was used.

## On this page

- [Show me](#show-me) — three concrete examples, side by side
- [Comparison: when to use which](#comparison-when-to-use-which)
- [Digest pinning: why it matters](#digest-pinning-why-it-matters)
- [What gets stored locally](#what-gets-stored-locally)
- [Switching install method](#switching-install-method-for-the-same-provider)
- [Registry entries with both](#registry-entries-can-declare-both)

---

## Show me

Three ways to install a provider. Pick the one that fits your workflow.

### 1. Install from the registry (git — default)

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
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0@sha256:abc123...
```

Notice the `@sha256:` digest. It's required — tags can be silently re-rolled, so digest pinning is how you get reproducible installs.

---

## Comparison: when to use which

| | Git (build from source) | Docker image |
|---|---|---|
| **Requires on host** | Go toolchain, git | Docker daemon only |
| **Distribution** | git repo (+ registry for name lookup) | container registry (ghcr, dockerhub, private) |
| **Digest pinning** | commit SHA (if you use it) | image `@sha256:` digest (required) |
| **Review the code** | yes — clone is on disk | no — binary in image |
| **Fork / patch locally** | easy | requires rebuilding the image |
| **Air-gapped / corporate registries** | via private git | via private image registry |
| **Typical user** | provider authors, development | operators, production deployment |

---

## Digest pinning: why it matters

When you install from a Docker image, you *must* pin by digest: `@sha256:abc123...`. Why?

Tags like `:0.2.0` or `:latest` can be re-rolled without warning. Today's `ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0` could be rebuilt tomorrow with a security fix or breaking change, and you'd get a silent upgrade on your next install or Docker pull. That's a problem for reproducibility and audit trails.

Digests never move. `@sha256:abc123` always points to the same image layers, byte-for-byte. If a new version ships, you get a new digest — then you upgrade explicitly, on your schedule, after reviewing the changes.

Git installs support this too (via commit SHA), but it's optional. Docker image installs require it because binary distributions are opaque by nature — you can't read the code to assess the change, so the digest is your anchor.

---

## What gets stored locally

Both methods produce the same layout under `~/.mgtt/providers/<name>/`:

```
~/.mgtt/providers/tempo/
├── .mgtt-install.json     # metadata: method, digest/commit, timestamp
├── probe                  # the executable (from either git build or docker extract)
└── <maybe other files>    # docs, examples, etc (git install only)
```

The `.mgtt-install.json` file records:

```json
{
  "name": "tempo",
  "method": "image",
  "image_ref": "ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0@sha256:abc123...",
  "installed_at": "2025-04-16T10:30:00Z"
}
```

Or for git:

```json
{
  "name": "tempo",
  "method": "git",
  "url": "https://github.com/mgt-tool/mgtt-provider-tempo",
  "commit": "a1b2c3d...",
  "installed_at": "2025-04-16T10:30:00Z"
}
```

The `mgtt provider list` command surfaces this:

```bash
$ mgtt provider list
tempo    0.2.0    git     https://github.com/mgt-tool/mgtt-provider-tempo#a1b2c3d
quickwit 0.1.5    image   ghcr.io/mgt-tool/mgtt-provider-quickwit:0.1.5@sha256:def456
```

---

## Switching install method for the same provider

Both methods use the same local directory structure, so switching is straightforward:

1. **Uninstall** the old method:
   ```bash
   mgtt provider uninstall tempo
   ```

2. **Reinstall** using the new method:
   ```bash
   mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0@sha256:abc123...
   ```

There's no data to migrate — the Go binary and the Docker image are just implementations of the same interface. The only gotcha: if you uninstall from an image, the uninstall output will print a helpful `docker rmi` hint:

```
Uninstalled tempo (image method)
To clean up the image:
  docker rmi ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0@sha256:abc123...
```

That's optional; the image won't interfere with future installs.

---

## Registry entries can declare both

The public registry (`docs/registry.yaml`) supports an optional `image:` field alongside `url:`. Either field can seed `mgtt provider install <name>`:

```yaml
tempo:
  url: https://github.com/mgt-tool/mgtt-provider-tempo
  image: ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0@sha256:abc123...
  description: Per-span SLO checks against Grafana Tempo
  tags:
    - tracing
    - otel
```

When you run `mgtt provider install tempo`, it uses the git URL by default. If you pass `--image`, the image field takes priority:

```bash
mgtt provider install tempo           # uses url:
mgtt provider install --image tempo   # uses image: (if present in registry)
```

(Note: auto-preferring the image when the registry has both is a future enhancement. Today `--image` must be passed explicitly.)

---

## See also

- [Multi-File Models](./multi-file-models.md) — the other provider methodology doc: when and how to split a system into several model files
