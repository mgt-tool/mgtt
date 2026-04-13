# Provider Vocabulary

The vocabulary (`provider.yaml`) tells mgtt's constraint engine what your technology looks like — what component types exist, what facts can be observed, what states are possible, and how failures propagate.

---

## Full schema

```yaml
meta:
  name: my-provider
  version: 0.1.0
  description: One-line description of what this provider covers
  requires:
    mgtt: ">=1.0"
  command: "$MGTT_PROVIDER_DIR/bin/mgtt-provider-my-provider"

hooks:
  install: hooks/install.sh

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

---

## Section reference

### `meta`

Provider identity and binary location.

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Lowercase, hyphen-separated. Unique across the ecosystem. |
| `version` | yes | Semver string. |
| `description` | yes | One-line description. |
| `requires.mgtt` | yes | Minimum mgtt version (semver range). |
| `command` | yes | Path to the provider binary. `$MGTT_PROVIDER_DIR` is substituted at runtime with the provider's install directory. Empty string for vocabulary-only providers. |

### `hooks`

| Field | Required | Description |
|-------|----------|-------------|
| `install` | no | Path to the install script, run during `mgtt provider install`. See [Install Hooks](hooks.md). |

### `auth`

Documents what credentials the provider needs. mgtt never touches credentials — this is for the human (or AI) reading the provider definition.

| Field | Description |
|-------|-------------|
| `strategy` | How auth works: `environment`, `config-file`, `token`, etc. |
| `reads_from` | List of environment variables or config file paths. |
| `access.probes` | What probing requires (e.g., `kubectl read-only`, `AWS API read-only`). |
| `access.writes` | What write operations require. Usually `none`. |

### `variables`

Parameters the model author sets in `meta.vars`. Substituted into probe commands as `{variable_name}`.

| Field | Required | Description |
|-------|----------|-------------|
| `description` | yes | What this variable controls. |
| `required` | no | Whether the model must provide this variable. Default: `false`. |
| `default` | no | Default value if not provided. |

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
| `parse` | How to parse stdout: `int`, `float`, `bool`, `string`, `exit_code`, `json:<path>`, `lines:<N>`, `regex:<pattern>` |
| `cost` | `low`, `medium`, `high` |
| `access` | Human-readable access description |

A provider can be **vocabulary-only** (no binary, no install hook) if all facts have inline `probe.cmd` definitions. This is the quick-start path for prototyping.

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

Checks: YAML syntax, state ordering, fact types resolve against stdlib, failure_modes reference declared states, expressions parse correctly.

---

## Next steps

- [Binary Protocol](protocol.md) — implementing the probe/validate/describe commands
- [Install Hooks](hooks.md) — Go, Python, and pre-compiled examples
- [Testing](testing.md) — validate, simulate, and live-test your provider
