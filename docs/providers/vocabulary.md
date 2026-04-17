# Provider Vocabulary

The vocabulary (`provider.yaml`) tells mgtt's constraint engine what your technology looks like — what component types exist, what facts can be observed, what states are possible, how failures propagate — plus the provider's own operational metadata (capabilities, network mode, compatibility binding, variables, auth, hooks).

## On this page

- [Full schema](#full-schema)
- [Section reference](#section-reference) — meta, needs, network, compatibility, hooks, variables, auth, types, facts, states, failure_modes
- [Validate your vocabulary](#validate-your-vocabulary)
- [Next steps](#next-steps)

---

## Full schema

```yaml
meta:
  name: my-provider
  version: 0.1.0
  description: One-line description of what this provider covers
  tags: [databases, cloud]
  requires:
    mgtt: ">=1.0"
  command: "$MGTT_PROVIDER_DIR/bin/mgtt-provider-my-provider"

needs: [kubectl, aws]
network: host

compatibility:
  backend: my-backend
  versions: ">=2.6.0,<2.7.0"
  tested_against:
    - "my-backend:2.6.0@sha256:abc…"
  notes: |
    Optional prose describing contract subtleties, response-shape changes
    across minor versions, etc.

hooks:
  install: hooks/install.sh
  uninstall: hooks/uninstall.sh

auth:
  strategy: environment
  reads_from:
    - MY_TOOL_CONFIG
    - ~/.my-tool/config
  access:
    probes: read-only
    writes: none

variables:
  namespace:
    description: target namespace
    required: false
    default: default

types:
  server:
    description: A server instance

    facts:
      connected:
        type: mgtt.bool
        ttl: 15s
        cost: low
        access: network read

      response_time:
        type: mgtt.float
        ttl: 30s
        cost: low

    healthy:
      - connected == true
      - response_time < 500

    states:
      live:
        when: "connected == true & response_time < 500"
        description: responding normally
      degraded:
        when: "connected == true & response_time >= 500"
        description: slow responses
      stopped:
        when: "connected == false"
        description: not responding

    default_active_state: live

    failure_modes:
      degraded:
        can_cause: [timeout, upstream_failure]
      stopped:
        can_cause: [upstream_failure, connection_refused]
```

Every top-level key above is optional except `meta`. Shell-fallback providers (vocabulary-only, no binary) omit `needs`, `network`, `hooks.install`, and set `meta.command: ""`.

---

## Section reference

### `meta`

Provider identity and binary location.

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Lowercase, hyphen-separated. Unique across the ecosystem. |
| `version` | yes | Semver string. |
| `description` | yes | One-line description. |
| `tags` | no | Loose subject labels (`[databases, tracing, iac, …]`). Mirrored in the public registry for search. |
| `requires.mgtt` | yes | Minimum mgtt version (semver range, e.g. `">=0.1.4"`). |
| `command` | yes | Path to the provider binary. `$MGTT_PROVIDER_DIR` is substituted at runtime with the provider's install directory. Empty string (`""`) for vocabulary-only providers with inline shell probes. |

### `needs`

Optional. Lists named **capabilities** the provider requires at probe time. Each label names a host-side package, credential chain, or socket (`kubectl`, `aws`, `docker`, `terraform`, `gcloud`, `azure`). Top-level because it's a provider-level property: git installs satisfy needs by inheriting the operator's shell environment; image installs satisfy them via `docker run` bind mounts and env forwards built by the capability vocabulary.

```yaml
needs: [kubectl, aws]
```

Omit when the provider reads nothing from the host (HTTP-only providers configured entirely through `vars:`). Shell-fallback providers (no `meta.command`) must omit it — there's no binary to attach the forwards to.

See the [Provider Capabilities reference](../reference/image-capabilities.md) for the full built-in vocabulary, operator-override file, and opt-out mechanics.

### `network`

Optional. Selects the docker-run network mode for image-installed providers. Valid values: `bridge` (default), `host`, `none`.

```yaml
network: host
```

Separate from `needs:` because network mode is a **runtime isolation setting**, not a host-resource grant — mixing the two conflated categories. Git-installed providers ignore this field entirely; they run in the operator's native namespace.

- **`bridge`** (default): container gets a NAT'd virtual NIC. Reaches the internet, not the host's localhost or private networks. Right for providers whose only backend is an external HTTPS endpoint.
- **`host`**: container shares the host's network namespace. Reaches in-cluster DNS (`*.svc`), private interfaces, localhost services. Required for Kubernetes (cluster API), Terraform (private state backends), anything reaching `host.docker.internal` or a VPN'd target.
- **`none`**: no network at all. Mostly a security posture for probes that exercise pure local state.

Validated by `mgtt provider validate` — typos (`overlay`, `bridged`) fail loudly at authoring time.

### `compatibility`

Optional. Binds the provider to specific backend versions it's been built and tested against. Protects against silent breakage when a minor-version response shape changes.

```yaml
compatibility:
  backend: tempo
  versions: ">=2.6.0,<2.7.0"
  tested_against:
    - "grafana/tempo:2.6.0@sha256:f55a8a…"
  notes: |
    Tempo 2.6 introduced breaking changes vs. 2.5:
      • Metrics response shape: {"data":…} → {"series":…}
      • Percentile syntax order: (p, duration) → (duration, p)
```

| Field | Description |
|-------|-------------|
| `backend` | Name of the backend system (matches the tool/service, e.g. `tempo`, `quickwit`, `docker`). |
| `versions` | Semver range the provider supports. Use it. Backends re-roll tags. |
| `tested_against` | List of SHA-pinned image refs the provider's integration tests run against. Digest pin — tag pins are vulnerable to silent re-rolls. |
| `notes` | Free-form markdown. Use it to document response-shape quirks, missing features in older versions, etc. |

Omit the block for providers with no backend-version sensitivity (e.g. `mgtt-provider-aws`, which uses the AWS API — stable across releases).

### `hooks`

| Field | Required | Description |
|-------|----------|-------------|
| `install` | no | Path to a script run during `mgtt provider install`. Typically builds the binary from source (`go build -o bin/…`). Image installs skip hooks entirely. See [Install Hooks](hooks.md). |
| `uninstall` | no | Path to a script run during `mgtt provider uninstall <name>` before the provider directory is removed. Cleans build artifacts, deregisters credentials, etc. If the script fails, the directory is still removed — uninstall must always succeed. |

### `auth`

Documents what credentials the provider needs. mgtt never touches credentials — this is for the human (or AI) reading the provider definition, and for `mgtt provider validate` to surface the access posture.

| Field | Description |
|-------|-------------|
| `strategy` | How auth works: `environment`, `config-file`, `token`, `none`, etc. |
| `reads_from` | List of environment variables or config-file paths the provider inspects. Treat as documentation, not enforcement. |
| `access.probes` | What probing requires (e.g., `kubectl read-only`, `AWS API read-only`, `tempo HTTP read`). |
| `access.writes` | What write operations require. `none` for pure-read providers. Non-`none` triggers a yellow WARN during validation — operators must confirm the credential scope matches. |

### `variables`

Parameters the model author sets in `meta.vars` (model-side) or as `vars:` on a component. Substituted into probe commands as `{variable_name}`; passed to binary providers as `--<name> <value>` flags.

| Field | Required | Description |
|-------|----------|-------------|
| `description` | yes | What this variable controls. |
| `required` | no | Whether the model must provide this variable. Default: `false`. |
| `default` | no | Default value when the model doesn't set one. |

### `types`

The component types your technology has. Each type declares facts, health conditions, states, and failure modes.

#### `types.<name>.facts`

Observable properties of this component type.

| Field | Required | Description |
|-------|----------|-------------|
| `type` | yes | Stdlib type: `mgtt.int`, `mgtt.float`, `mgtt.bool`, `mgtt.string`, etc. See [Type Catalog — stdlib](../reference/type-catalog.md#stdlib-primitive-types). |
| `ttl` | yes | Staleness threshold (e.g., `30s`, `5m`). After this period, the fact is considered stale and re-probed. |
| `cost` | no | Probe cost: `low`, `medium`, `high`. Used by the engine to rank probes. |
| `access` | no | Human-readable description of required access (e.g., `kubectl read-only`). |
| `probe` | no | Inline probe definition. See below. |

#### Inline probe definitions

For providers without a compiled binary, facts can define inline shell probes:

```yaml
facts:
  ready_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get deploy {name} -o jsonpath={.status.readyReplicas}"
      parse: int
      cost: low
      access: kubectl read-only
```

| Field | Description |
|-------|-------------|
| `cmd` | Shell command. `{name}` is the component name, `{variable}` for provider variables. |
| `parse` | How to parse stdout: `int`, `float`, `bool`, `string`, `exit_code`, `json:<path>`, `lines:<N>`, `regex:<pattern>`. |
| `cost` | `low`, `medium`, `high`. |
| `access` | Human-readable access description. |

A provider can be **vocabulary-only** (no binary, no install hook) if all facts have inline `probe.cmd` definitions. This is the quick-start path for prototyping. Vocabulary-only providers cannot be installed via `--image` — there's no entrypoint for mgtt to invoke.

#### `types.<name>.healthy`

Conditions that must ALL hold for the component to be healthy. Uses mgtt's expression syntax:

```yaml
healthy:
  - connected == true
  - response_time < 500
```

Operators: `==`, `!=`, `<`, `>`, `<=`, `>=`. Compound: `&` (and), `|` (or).

#### `types.<name>.states`

Ordered list of possible states. **Evaluated top-to-bottom — first match wins.**

```yaml
states:
  degraded:
    when: "ready_replicas < desired_replicas & restart_count > 5"
    description: crash-looping
  starting:
    when: "ready_replicas < desired_replicas"
    description: pods initialising
  live:
    when: "ready_replicas == desired_replicas"
    description: all replicas ready
```

!!! warning "State ordering matters"
    Put specific states before general ones. `degraded` (needs two conditions) must come before `starting` (needs one), otherwise `starting` matches first and `degraded` is unreachable. `mgtt provider validate` catches this.

#### `types.<name>.default_active_state`

The "normal" state. Components in this state are considered healthy by the engine.

#### `types.<name>.failure_modes`

For each non-healthy state, what downstream effects it can cause:

```yaml
failure_modes:
  degraded:
    can_cause: [upstream_failure, timeout, connection_refused]
  stopped:
    can_cause: [upstream_failure, connection_refused]
```

Values from the [standard failure mode vocabulary](../reference/type-catalog.md#standard-failure-mode-vocabulary).

---

## Validate your vocabulary

```bash
mgtt provider validate ./my-provider
```

Checks: YAML syntax, state ordering, fact types resolve against stdlib, failure_modes reference declared states, expressions parse correctly, every `needs:` entry is in the capability vocabulary, `network:` value is one of `bridge`/`host`/`none`, shell-fallback providers don't declare `needs:`, `meta.requires.mgtt` is satisfied.

---

## Next steps

- [Binary Protocol](protocol.md) — implementing the probe/validate/describe commands
- [Install Hooks](hooks.md) — Go, Python, and pre-compiled examples
- [Testing](testing.md) — validate, simulate, and live-test your provider
- [Provider Capabilities](../reference/image-capabilities.md) — full built-in vocabulary and operator-override mechanics
