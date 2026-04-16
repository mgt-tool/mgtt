# Configuration Reference

mgtt is configured exclusively via **environment variables** (and a small set of CLI flags that mirror the most common ones for per-invocation overrides). There is no `mgtt.config` file. This page is the canonical list — if it's not here, mgtt does not read it.

The Twelve-Factor reasoning: env vars compose well with shells, container orchestrators, secrets managers, and CI systems. A single config file would be a second source of truth that drifts from the env, splits operator mental models, and tempts scope creep.

---

## Quick reference

| Variable | Purpose | Default |
|---|---|---|
| `MGTT_HOME` | Where mgtt looks for installed providers | `~/.mgtt/` |
| `MGTT_REGISTRY_URL` | Registry index URL (or `disabled` / `none` / `off`, or `file://...`) | community registry on GitHub Pages |
| `MGTT_FIXTURES` | Path to a fixture YAML; switches probes from real backends to recorded data | unset (use real backends) |
| `MGTT_DEBUG` | Set to `1` to emit per-probe trace lines on stderr | unset (silent) |
| `MGTT_PROBE_TIMEOUT` | Per-probe timeout (`time.ParseDuration` syntax — e.g. `45s`, `2m`) | runner-default 30s |
| `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY` | Standard Go HTTP proxy chain — honored by registry fetch and provider clones | unset |
| `SSL_CERT_FILE` / `SSL_CERT_DIR` | Standard Go TLS CA bundle override | system trust store |

---

## `MGTT_HOME` — install location

Where `mgtt provider install` writes provider directories, where `provider ls` / `inspect` / `validate` look them up, and where the registry-fetch cache lives (`$MGTT_HOME/cache/registry/<sha>.yaml`, keyed on the registry URL so distinct sources don't cross-contaminate).

```bash
export MGTT_HOME=/opt/mgtt
mgtt provider install kubernetes
# → /opt/mgtt/providers/kubernetes/
```

Search order for installed providers (first hit wins):
1. `$MGTT_HOME/providers/`
2. `~/.mgtt/providers/`
3. `./providers/` (local repo)

Useful for: shared multi-tenant installs (point everyone at a read-only `/opt/mgtt/providers/`) and per-environment isolation (different `MGTT_HOME` per CI job).

---

## `MGTT_REGISTRY_URL` — alternative or no registry

By default, `mgtt provider install <name>` resolves the name through the community registry at `https://mgt-tool.github.io/mgtt/registry.yaml`. Override it for corporate scenarios:

### Disable the registry entirely

For air-gapped networks or shops that refuse external code resolution:

```bash
export MGTT_REGISTRY_URL=disabled    # or "none" or "off" — case-insensitive
mgtt provider install kubernetes     # → fails with actionable error
mgtt provider install https://internal.git/mgtt-provider-kubernetes  # works
mgtt provider install /opt/staged-providers/kubernetes               # works
```

When disabled, `provider install` accepts only **git URLs and local paths** for *new* installs — bare names produce: `registry: disabled by configuration: "kubernetes" is not a git URL or local path — pass one explicitly`.

> **Note:** if a provider with that name is *already installed* under `$MGTT_HOME/providers/`, the local copy is reused without consulting the registry — `--registry disabled` only gates the upstream fetch step. To force re-install from a specific source, run `mgtt provider uninstall <name>` first, then re-install with an explicit URL/path.

> **Note:** an unset `MGTT_REGISTRY_URL` and an empty `MGTT_REGISTRY_URL=""` both fall back to the community default. To disable, use one of the explicit sentinels (`disabled` / `none` / `off`).

### Internal mirror (HTTPS)

Mirror the index on Artifactory, an S3 bucket, or any HTTPS server:

```bash
export MGTT_REGISTRY_URL=https://artifactory.corp/mgtt/registry.yaml
mgtt provider install kubernetes
# → fetches the index from your mirror, clones the URLs the mirror lists
```

The mirror serves the same YAML schema as the community registry — it's just a different location. `HTTPS_PROXY` / `SSL_CERT_FILE` apply normally.

### Local file (truly air-gapped)

For installs where even your mirror is unreachable, ship the registry alongside:

```bash
export MGTT_REGISTRY_URL=file:///opt/mgtt/registry.yaml
mgtt provider install kubernetes    # reads the file, clones each entry's URL
```

Supported forms: `file:///absolute/path` and `file://localhost/absolute/path`. Other authority components are rejected. Percent-encoding is honored, so `file:///opt/my%20registry.yaml` works for paths containing spaces. **Unix paths only** — Windows path mapping is not implemented.

> **For TRUE air-gap:** the registry tells mgtt *where to fetch each provider*, but `provider install <name>` then `git clone`s the URL listed in that entry. If your registry entries point at GitHub, air-gapped installs will still fail at the clone step. For full air-gap, ship a registry whose entries point at internal git URLs.

### Per-invocation override

The `--registry` flag on `mgtt provider install` overrides the env var for one command — useful in scripts that need to switch sources:

```bash
mgtt provider install --registry https://staging-registry.corp/r.yaml my-internal-provider
mgtt provider install --registry disabled https://github.com/internal/provider.git
```

Precedence: `--registry` > `MGTT_REGISTRY_URL` > default.

---

## `MGTT_FIXTURES` — record/replay mode

Replay a recorded set of probe outputs instead of executing them against real backends. Used in CI and demos.

```bash
export MGTT_FIXTURES=./scenarios/incident-2026-04-15.yaml
mgtt plan
# → no kubectl, no aws, no network — probes return the recorded values
```

The fixture YAML maps `provider → component → fact → {stdout, exit, status}`. See `internal/providersupport/probe/fixture/` for the schema.

---

## `MGTT_DEBUG` — probe boundary tracing

```bash
MGTT_DEBUG=1 mgtt plan 2>&1 | grep '\[mgtt'
[mgtt 14:22:03.401] probe start: /home/x/.mgtt/providers/kubernetes/bin/mgtt-provider-kubernetes web.ready_replicas (type=workload vars=1 extra=0)
[mgtt 14:22:03.598] probe end: ...kubernetes status=ok parsed=3
```

One line per probe invocation, on stderr, with timing. Format intentionally **does not name backend keys** — only counts of `vars` and `extra` map entries — so the trace itself doesn't leak `namespace`/`region`/`cluster` vocabulary.

---

## `MGTT_PROBE_TIMEOUT` — per-probe timeout

Default is the runner's built-in 30s. Bump for slow backends (e.g. terraform `plan` against cloud APIs, large EKS clusters):

```bash
MGTT_PROBE_TIMEOUT=120s mgtt plan
```

Accepts any [`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration) form: `750ms`, `45s`, `2m`, `1h30m`. Unparseable values (e.g. `"60"` without a unit) emit a one-time warning to stderr and fall back to the default — your typo is loud rather than silent.

---

## HTTP proxy + TLS

mgtt does not implement its own HTTP stack — it uses Go's `net/http`, which honors the standard environment chain:

| Variable | Effect |
|---|---|
| `HTTPS_PROXY=https://corp-proxy:8080` | All HTTPS calls (registry fetch, `git clone` over HTTPS) go through the proxy |
| `HTTP_PROXY=...` | Same for HTTP |
| `NO_PROXY=.corp,localhost` | Comma-separated suffix list; matches bypass the proxy |
| `SSL_CERT_FILE=/etc/ssl/corp-ca.pem` | Use this PEM bundle instead of the system trust store |
| `SSL_CERT_DIR=/etc/ssl/corp-trust/` | Trust every PEM in this directory |

For `git clone` (used by `provider install` against URLs), proxy + TLS settings are honored by `git` itself, not mgtt — configure them in `~/.gitconfig` or via the standard env vars `git` reads.

---

## What mgtt does NOT call out to

For security review:

- **No telemetry.** mgtt makes zero outbound calls except those you explicitly invoke: `provider install` (registry HTTPS + git clone), and the provider runner binaries (which call your own backend tools — kubectl, aws, terraform, …).
- **No auto-update.** mgtt does not check for new versions on its own.
- **No analytics.** No `mgtt --version` ping, no usage counters, no error reporters.

If your security review needs "what does this binary talk to in the worst case," the answer is exactly: `MGTT_REGISTRY_URL` (over the configured proxy), the git URLs in that registry, and whatever the operator-installed provider binaries decide to invoke.

---

## Setting env vars per-shell vs. globally

For one command:

```bash
MGTT_REGISTRY_URL=disabled mgtt provider install ./local-provider
```

For a session:

```bash
export MGTT_REGISTRY_URL=https://artifactory.corp/mgtt/registry.yaml
export MGTT_HOME=/opt/mgtt
mgtt provider install kubernetes
mgtt plan
```

For a system-wide install, add the exports to `/etc/profile.d/mgtt.sh` or your container's entrypoint script.

In Kubernetes Deployments / CronJobs:

```yaml
env:
  - name: MGTT_HOME
    value: /opt/mgtt
  - name: MGTT_REGISTRY_URL
    value: file:///opt/mgtt/registry.yaml
  - name: MGTT_PROBE_TIMEOUT
    value: 60s
```
