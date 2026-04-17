# Provider Install Methods

Two ways to install a provider — git clone and build, or pull a Docker image. Both produce the same `~/.mgtt/providers/<name>/` layout; `mgtt plan` doesn't care which method was used.

## On this page

- [Show me](#show-me) — three concrete examples, side by side
- [Comparison: when to use which](#comparison-when-to-use-which)
- [Digest pinning: why it matters](#digest-pinning-why-it-matters)
- [What gets stored locally](#what-gets-stored-locally)
- [How image-installed providers reach the host — capabilities](#how-image-installed-providers-reach-the-host-capabilities)
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

Tags like `:0.2.0` or `:latest` can be re-rolled without warning. Today's `ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0` could be rebuilt tomorrow with a security fix or breaking change, and you'd get a silent upgrade on your next install or Docker pull. That's a problem for reproducibility and audit trails. This isn't hypothetical — the `grafana/tempo:2.6.0` image tag was rebuilt in place and broke mgtt-provider-tempo's existing deployments. See the discussion in the CHANGELOG.md of that provider.

Digests never move. `@sha256:abc123` always points to the same image layers, byte-for-byte. If a new version ships, you get a new digest — then you upgrade explicitly, on your schedule, after reviewing the changes.

Git installs support this too (via commit SHA), but it's optional. Docker image installs require it because binary distributions are opaque by nature — you can't read the code to assess the change, so the digest is your anchor.

---

## What gets stored locally

Both methods write into `~/.mgtt/providers/<name>/`, but what ends up on disk differs:

**Git install:**
```
~/.mgtt/providers/tempo/
├── .mgtt-install.json     # metadata: method, source URL, timestamp
├── probe                  # the compiled executable (built from source)
└── <maybe other files>    # provider.yaml, docs, examples, etc.
```

**Image install:**
```
~/.mgtt/providers/tempo/
├── .mgtt-install.json     # metadata: method, image digest, timestamp
└── provider.yaml          # provider descriptor only; binary lives in the Docker image
```

The `.mgtt-install.json` file records:

```json
{
  "method": "image",
  "source": "ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0@sha256:abcdef...",
  "installed_at": "2026-04-17T10:30:00Z",
  "version": "0.2.0"
}
```

Or for git:

```json
{
  "method": "git",
  "source": "https://github.com/mgt-tool/mgtt-provider-tempo",
  "installed_at": "2026-04-17T10:30:00Z",
  "version": "0.2.0"
}
```

The `mgtt provider list` command surfaces this:

```bash
$ mgtt provider list
✓ tempo    v0.2.0    git     Per-span SLO checks against Grafana Tempo
✓ quickwit v0.1.5    image   Cross-span tracing checks against Quickwit
```

---

## How image-installed providers reach the host — capabilities

### The problem

A git-installed provider is a native process. It inherits the operator's entire environment for free: `KUBECONFIG` is set, `~/.aws/credentials` is readable, `$PWD` is the Terraform workdir, `/var/run/docker.sock` is right there.

An image-installed provider is a container. `docker run --rm <image>` inherits **none** of that by default. A Kubernetes provider launched this way can't reach the cluster; a Terraform provider can't see its state directory; a Docker provider can't talk to the daemon. The image ships with `kubectl` / `terraform` / `docker` on `PATH`, but the *context* those binaries need is on the host side of the boundary.

### The rejected fixes

Two obvious approaches — both wrong:

- **Inherit everything.** `docker run --network host -v $HOME:$HOME:ro -e ...`. Simple and it works, but you've just mounted `~/.ssh/`, `~/.gnupg/`, and every token you own into a container whose binary was pulled from a third-party registry. Digest pinning is not a strong enough trust anchor to justify that.
- **Per-provider `env: [...] mounts: [...]` schema.** Precise but verbose; every provider author re-types the same `~/.kube:/root/.kube:ro` incantations; drift between `mgtt-provider-kubernetes` and `mgtt-provider-quickwit`'s YAML re-introduces bugs that should be fixed in one place.

### The design — named capabilities

Providers declare **semantic labels** in `provider.yaml`:

```yaml
image:
  needs: [kubectl, network]
```

mgtt owns the vocabulary — the mapping from label to `docker run` flags:

| Label | Expands to |
|---|---|
| `kubectl` | `-v $HOME/.kube:/root/.kube:ro -e KUBECONFIG=…` |
| `aws` | `-v $HOME/.aws:/root/.aws:ro -e AWS_PROFILE -e AWS_ACCESS_KEY_ID …` |
| `docker` | `-v /var/run/docker.sock:/var/run/docker.sock` |
| `terraform` | `-v $PWD:/workspace -w /workspace -e TF_VAR_* …` |
| `network` | `--network host` |
| `gcloud` | `-v $HOME/.config/gcloud:/root/.config/gcloud:ro …` |
| `azure` | `-v $HOME/.azure:/root/.azure:ro …` |

This is the same pattern snap (`plugs: [docker-support]`) and flatpak (`--socket=docker`) use: the application names a capability, the packaging system maps it to syscalls. Three properties fall out that the alternatives don't get:

1. **Scannable.** `needs: [docker]` is one word an operator reads in a second. A list of `-v`/`-e` flags is not. Open `provider.yaml` on GitHub, see the caps at the top.
2. **Consistent.** Every kubectl-wrapping provider forwards the same files. No drift between provider authors re-implementing the same mount string.
3. **Bounded.** The socket mount only happens when `docker` is declared. A provider can't silently mount your `~/.ssh/`.

### What the operator sees

Every surface that lists providers now shows capabilities:

- `provider.yaml` — `image:` block at the top, right after `meta:`.
- [`docs/registry.yaml`](../reference/registry.md) — `capabilities: [...]` per registry entry.
- `mgtt provider install --image …` prints `→ capabilities: kubectl, network` before the install completes.
- `mgtt provider ls` shows a bracketed cap column per installed provider.

### Extending the vocabulary

The built-in set is deliberately small — the seven caps above cover every provider mgtt ships with. Operators with non-default paths, air-gapped setups, or internal providers extend it locally without a mgtt change.

**Override a built-in.** Drop a file at `$MGTT_HOME/capabilities.yaml`:

```yaml
capabilities:
  # Non-default kubeconfig location on our bastion hosts
  kubectl:
    - "-v"
    - "/etc/kubernetes/admin.conf:/root/.kube/config:ro"
    - "-e"
    - "KUBECONFIG=/root/.kube/config"
```

**Define a custom cap.** Same file:

```yaml
capabilities:
  tibco:
    - "-v"
    - "/etc/tibco/cert.pem:/root/cert.pem:ro"
    - "-e"
    - "TIBCO_BROKER_URL"
```

A provider that declares `needs: [tibco]` will now resolve against the operator's file. Precedence (highest wins): `MGTT_IMAGE_CAP_<NAME>` env var → operator file → built-in.

**Refuse a cap.** For locked-down environments:

```
MGTT_IMAGE_CAPS_DENY=docker,aws
```

mgtt skips those caps at probe time regardless of what the provider declared; the probe runs without them and fails with an honest error. This is the right knob for a CI where you don't want `mgtt plan` to wield the Docker socket even if a provider asks.

### When does a cap become a built-in?

Operator-local definitions are perfect for one-off infrastructure. But if a capability generalizes — a new cloud, a new observability backend, a new secret store that many providers will want — it's worth upstreaming into mgtt's built-in map so every provider author picks it up for free. The criterion is usage, not novelty: two providers needing the same forward is enough to justify a built-in.

See the [Image Capabilities reference](../reference/image-capabilities.md) for the full vocabulary, the YAML schema, env-var shortcuts, and failure modes.

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

When you run `mgtt provider install tempo`, it uses the git URL by default. The `image:` field in the registry is a placeholder for future enhancements — today `--image` requires an explicit, fully-qualified image ref with a digest:

```bash
mgtt provider install tempo           # uses url: (git clone and build)
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0@sha256:abc123...   # requires full ref with @sha256: digest
```

---

## See also

- [Multi-File Models](./multi-file-models.md) — the other provider methodology doc: when and how to split a system into several model files
