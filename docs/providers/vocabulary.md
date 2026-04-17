# Provider Vocabulary

The vocabulary (`manifest.yaml`) tells mgtt's constraint engine what your technology looks like — what component types exist, what facts can be observed, what states are possible, how failures propagate — plus the provider's own operational metadata (capabilities, network mode, compatibility binding, variables, auth, hooks).

## On this page

- [Full schema](#full-schema)
- [Section reference](#section-reference) — meta, needs, network, compatibility, hooks, read_only, writes_note, variables, types, facts, states, failure_modes
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

# read_only defaults to true; omit when you're read-only. Set to false
# when the provider has side effects, and describe them in writes_note.
#
# read_only: false
# writes_note: |
#   The `drifted` fact runs `terraform plan` which refreshes state.

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

Optional. Capabilities the provider requires at probe time. Each label names a host-side package, credential chain, or socket.

```yaml
needs: [kubectl, aws]
```

Built-in labels: `kubectl`, `aws`, `docker`, `terraform`, `gcloud`, `azure`. Omit entirely when the provider reads nothing from the host. Providers with no `meta.command` cannot declare `needs:`.

See [Provider Capabilities](../reference/image-capabilities.md) for what each label expands to.

### `network`

Optional. Docker-run network mode for image-installed providers.

```yaml
network: host
```

| Value | Effect |
|---|---|
| `bridge` (default) | NAT'd external network. |
| `host` | Container shares the host's network namespace. Required for in-cluster DNS, private endpoints, `host.docker.internal`. |
| `none` | No network. |

Git-installed providers ignore this field.

### `compatibility`

Optional. Pins the provider to specific backend versions.

```yaml
compatibility:
  backend: tempo
  versions: ">=2.6.0,<2.7.0"
  tested_against:
    - "grafana/tempo:2.6.0@sha256:f55a8a…"
  notes: |
    Tempo 2.6 response shape:
      • Metrics: {"data":…} → {"series":…}
      • Percentile syntax: (p, duration) → (duration, p)
```

| Field | Description |
|-------|-------------|
| `backend` | Name of the backend system (e.g. `tempo`, `quickwit`, `docker`). |
| `versions` | Semver range the provider supports. |
| `tested_against` | SHA-pinned image refs the provider's integration tests run against. |
| `notes` | Free-form markdown. Document response-shape quirks, missing features per version. |

Omit for providers whose backend has a stable API across releases (e.g. the AWS API).

### `hooks`

| Field | Required | Description |
|-------|----------|-------------|
| `install` | no | Path to a script run during `mgtt provider install`. Typically builds the binary from source (`go build -o bin/…`). Image installs skip hooks entirely. See [Install Hooks](hooks.md). |
| `uninstall` | no | Path to a script run during `mgtt provider uninstall <name>` before the provider directory is removed. Cleans build artifacts, deregisters credentials, etc. If the script fails, the directory is still removed — uninstall must always succeed. |

### `read_only` and `writes_note`

Write posture. Both fields optional.

| Field | Default | Description |
|-------|---------|-------------|
| `read_only` | `true` | `true` = pure reader. `false` = the provider writes something. |
| `writes_note` | — | Prose describing the side effect. Required when `read_only: false`. Printed at install time. |

Default read-only case — omit both fields:

```yaml
meta: {…}
needs: [aws]
```

Provider with side effects:

```yaml
read_only: false
writes_note: |
  The `drifted` fact runs `terraform plan` which refreshes state — a
  write to the state backend. Other facts are pure reads. Bind a
  credential that cannot write to the state backend and omit the
  `drifted` fact for hard read-only.
```

Validation fails if `read_only: false` is declared without a `writes_note`. Install emits a WARN when non-default.

Document which env vars and config paths the provider reads in the provider's README, not here.

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
