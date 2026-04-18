# Using Providers

How to install, run, audit, and control providers with mgtt.

## On this page

- [Lifecycle](#lifecycle)
- [Invocation](#invocation) — how mgtt calls a provider
- [What mgtt forwards](#what-mgtt-forwards) — `runtime.needs`, `runtime.network_mode`, `read_only`
- [Operator controls](#operator-controls)
- [Auditing installed providers](#auditing-installed-providers)
- [Troubleshooting](#troubleshooting)

---

## Lifecycle

```
mgtt provider install <name|url|--image ref>
  ↓
~/.mgtt/providers/<name>/
├── manifest.yaml
├── types/
├── bin/provider        (git installs only)
└── .mgtt-install.json

mgtt plan
  ↓
For each probe: mgtt invokes the provider with
  probe <component> <fact> --type T [--flag val …]
Provider emits {"value": …, "raw": "…"} on stdout.

mgtt provider uninstall <name>
```

Every install method lands in the same on-disk layout, and every probe uses the same protocol envelope.

---

## Invocation

### Git install

```bash
<install-dir>/bin/provider probe <component> <fact> --type <type> [--var val …]
```

Runs as the operator's process. Inherits your shell environment, reads any files you can read.

### Image install

```bash
docker run --rm [--network <mode>] [<cap flags…>] <image-ref> \
  probe <component> <fact> --type <type> [--var val …]
```

The `--network`, cap flags, and image ref all come from the provider's `manifest.yaml` and `.mgtt-install.json`. Container starts with an empty environment unless `runtime.needs:` grants host-side resources explicitly.

---

## What mgtt forwards

Image-installed providers declare their runtime requirements in `manifest.yaml` under `runtime:`:

```yaml
runtime:
  needs: [kubectl, aws]
  network_mode: host
read_only: true
```

| Field | Values | What it controls |
|---|---|---|
| `runtime.needs` | `kubectl`, `aws`, `docker`, `terraform`, `gcloud`, `azure` | Bind mounts and env forwards for host tools, credential chains, and sockets. |
| `runtime.network_mode` | `bridge` (default), `host` | Container network mode. `host` is required to reach in-cluster DNS or host-local services. |
| `read_only` | `true` (default), `false` | Provider's write posture. `false` requires a `writes_note:` string describing the side effect. |

Each capability in `runtime.needs:` expands into specific `docker run` flags. Example for `needs: [kubectl]`:

```
-v $HOME/.kube:/root/.kube:ro -e KUBECONFIG=<your value>
```

Env forwards emit `-e KEY=VALUE` only when `KEY` is set in the caller. Full expansion table and override schema: [Provider Capabilities](../reference/image-capabilities.md).

---

## Operator controls

### `MGTT_HOME`

Base directory for installed providers, capability overrides, registry cache. Defaults to `$HOME/.mgtt`.

```bash
export MGTT_HOME=/opt/mgtt
```

### `MGTT_IMAGE_CAPS_DENY`

Refuse to inject specific capabilities at probe time, regardless of what the provider declared.

```bash
export MGTT_IMAGE_CAPS_DENY=docker,aws
```

Denied capabilities are omitted from the `docker run` line. The probe runs without them; expect a clear backend-side error when it tries to reach the missing resource.

### `MGTT_IMAGE_CAP_<NAME>`

Override the expansion of a single capability via env var. Argv is shell-split.

```bash
export MGTT_IMAGE_CAP_KUBECTL="-v /etc/kubernetes/admin.conf:/root/.kube/config:ro -e KUBECONFIG=/root/.kube/config"
```

### `$MGTT_HOME/capabilities.yaml`

Persistent overrides and custom capabilities. Drop-in shards at `$MGTT_HOME/capabilities.d/*.yaml` are also loaded.

```yaml
capabilities:
  kubectl:
    - "-v"
    - "/etc/kubernetes/admin.conf:/root/.kube/config:ro"
    - "-e"
    - "KUBECONFIG=/root/.kube/config"

  tibco:
    - "-v"
    - "/etc/tibco/cert.pem:/root/cert.pem:ro"
    - "-e"
    - "TIBCO_BROKER_URL"
```

Precedence: env var > operator file > built-in.

### `MGTT_PROBE_TIMEOUT`

Per-probe timeout. Default `30s`.

```bash
export MGTT_PROBE_TIMEOUT=120s
```

### `MGTT_REGISTRY_URL`

Registry index URL for name-to-URL resolution. Accepts `https://…`, `file:///path/to/registry.yaml`, or `disabled` / `none` / `off` (air-gapped mode — only git URLs and `--image` refs accepted).

---

## Auditing installed providers

```
$ mgtt provider ls
  ✓ kubernetes  v2.3.1  image  [kubectl]         Kubernetes cluster resources via kubectl
  ✓ tempo       v0.2.0  git    -                 Per-span SLO checks against Grafana Tempo
  ✓ terraform   v0.1.0  image  [terraform, aws]  Terraform-managed infrastructure
```

Columns: method (`git` | `image`), declared capabilities (`-` for none), description.

```
$ mgtt provider inspect <name>
```

Shows the full contract: posture, writes-note, needs, network, types, state machines, failure modes.

The [public registry](../reference/registry.md) lists every mgt-tool provider with capabilities and network columns.

---

## Troubleshooting

### Probe can't reach the cluster

Image-installed providers need `runtime.network_mode: host` to resolve in-cluster DNS or reach private API endpoints. Check the provider's declaration:

```bash
grep -A2 '^runtime:' $MGTT_HOME/providers/kubernetes/manifest.yaml
```

### A capability isn't being applied

Check, in order:

1. Is `MGTT_IMAGE_CAPS_DENY` set and listing the capability?
2. Was the provider installed via git? Capabilities only apply to image installs; `mgtt provider ls` shows the method.
3. Did the provider's `runtime.needs:` list change after install? Reinstall the provider.

### Refuse the Docker socket

```bash
export MGTT_IMAGE_CAPS_DENY=docker
```

Any provider declaring `runtime.needs: [docker]` runs without the socket mount. Relevant mainly for `mgtt-provider-docker`.

### An env var isn't being forwarded

`-e KEY=VALUE` fires only when `KEY` is set (non-empty) in the calling shell. Confirm:

```bash
env | grep AWS_
```

### Install fails with "unknown capability"

The provider's `manifest.yaml` declares a label not in the merged vocabulary. Add it to `$MGTT_HOME/capabilities.yaml` before reinstalling, or file an issue against the provider repo.

---

## See also

- [Provider Install Methods](provider-install-methods.md) — source vs image, digest pinning, on-disk layout
- [Provider Capabilities](../reference/image-capabilities.md) — full capability vocabulary and override schema
- [Writing Providers](../providers/overview.md) — authoring `manifest.yaml`, binary protocol, install scripts
- [manifest.yaml reference](../reference/manifest.md) — three-block schema, invariants, defaults
- [Configuration](../reference/configuration.md) — every `MGTT_*` env var
