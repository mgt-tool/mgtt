# Scenario Schema Reference

Complete reference for the hand-authored scenario files consumed by `mgtt simulate` — the YAML files that define failure simulations.

> Looking for the **generated** sidecar that `mgtt diagnose` reads? That's a different file: [`scenarios.yaml`](scenarios-yaml.md).

## On this page

- [Anatomy of a scenario](#anatomy-of-a-scenario)
- Fields:
    - [`name`](#name) — scenario name shown in output
    - [`description`](#description) — operator-facing notes
    - [`inject`](#inject) — synthetic facts fed to the engine
    - [`expect`](#expect) — what the engine should conclude
- [File location](#file-location)
- [Running scenarios](#running-scenarios)
- [Example: complete scenario set](#example-complete-scenario-set)
- [Common mistake: per-component status assertions](#common-mistake-per-component-status-assertions)
- [What a failing scenario teaches you](#what-a-failing-scenario-teaches-you)

---

## Anatomy of a scenario

```yaml
name: <string>              # required — scenario name (shown in output)
description: <string>       # optional — what this scenario tests and why

inject:                     # required — synthetic facts to feed the engine
  <component-name>:
    <fact-name>: <value>

expect:                     # required — what the engine should conclude
  root_cause: <component-name> | none
  path: [<component>, ...]  # optional — failure path from outermost to root cause
  eliminated: [<component>, ...]  # optional — components confirmed healthy
```

## Fields

### `name`

**Required.** Displayed in `mgtt simulate` output. Keep it short and descriptive.

```yaml
name: rds unavailable
```

### `description`

**Optional.** Explains what the scenario tests and why the expected outcome is correct. Useful for documentation — the engine ignores it.

```yaml
description: >
  rds stops accepting connections. api crash-loops as a result.
  engine should trace the fault to rds, not api.
```

### `inject`

**Required.** A map of component names to fact/value pairs. These synthetic facts replace what live probes would collect.

```yaml
inject:
  rds:
    available: false
    connection_count: 0
  api:
    ready_replicas: 0
    restart_count: 12
    desired_replicas: 3
```

**Fact names** must match facts defined by the provider for that component's type. See the [Type Catalog](type-catalog.md) for which facts each type exposes.

**Value types** match the fact's declared type:

| Fact type | Example values |
|-----------|---------------|
| `mgtt.bool` | `true`, `false` |
| `mgtt.int` | `0`, `42`, `500` |
| `mgtt.float` | `0.95`, `42.5` |
| `mgtt.string` | `"running"`, `"error"` |

**Components not listed in `inject`** are treated as having no observed facts. The engine cannot determine their state and will not eliminate them unless all their facts can be inferred from provider defaults.

!!! tip "Inject enough facts for state resolution"
    Each provider type has state definitions with conditions (e.g., `degraded: ready_replicas < desired_replicas & restart_count > 5`). If you inject `ready_replicas: 0` without `restart_count`, the engine may resolve the state as `starting` instead of `degraded`. See [Type Catalog](type-catalog.md) for each type's state conditions.

### `expect`

**Required.** Assertions about what the engine should conclude.

| Field | Required | Description |
|-------|----------|-------------|
| `root_cause` | yes | The component the engine identifies as root cause. Use `none` when all components are healthy. Asserted with strict equality. |
| `path` | no | The failure path from outermost component to root cause. Order: `[outermost, ..., root_cause]`. Asserted as an **ordered subsequence** (see below). |
| `eliminated` | no | Components confirmed healthy and removed from investigation. Asserted as a **subset** (see below). |

```yaml
expect:
  root_cause: rds
  path: [nginx, api, rds]
  eliminated: [frontend]
```

When `root_cause: none`, the engine should find no failures:

```yaml
expect:
  root_cause: none
  eliminated: [nginx, frontend, api, rds]
```

### How the matcher works

**`path` is an ordered subsequence.** Every component listed in `expected.path` must appear in the actual failure path *in the given order*. Extras between them are allowed.

```yaml
# scenario asserts:
expected.path: [nginx, api, rds]

# all of these pass:
actual: [nginx, api, rds]
actual: [nginx, api, legacy-gateway, rds]   # catalog source added a hop
actual: [edge-cf, nginx, api, rds]          # longer prefix

# these fail:
actual: [nginx, rds]                        # api missing
actual: [nginx, rds, api]                   # order wrong
```

You assert the *shape* of the chain, not its exact length. That makes scenarios durable under dependency-graph evolution: adding an intermediate component doesn't cascade-break scenarios whose real intent was "the chain runs through api on its way to rds".

**`eliminated` is a subset.** Every component listed must be in the actual eliminated set. The actual set may contain more.

```yaml
# scenario asserts:
expected.eliminated: [frontend, redis]

# passes — actual has extras:
actual.eliminated: [frontend, redis, payment-gateway, observability-collector]

# fails — missing an asserted component:
actual.eliminated: [frontend]
```

Order is ignored (set semantics). Adding a new topology-only component to the model doesn't invalidate every scenario's `eliminated:` list — it just means more things get eliminated, which is exactly what you'd expect.

### Why the relaxed semantics

Earlier mgtt versions asserted both `path` and `eliminated` with strict equality. Any model change — a new component, a split service, an extra intermediate hop from a catalog source — cascade-broke every scenario whether the logical root cause changed or not. The relaxed matcher asserts that the expected shape is *present* in the actual result, not that actual contains *nothing else*. Scenarios stay load-bearing (missing assertions still fail loudly) while becoming durable under the kind of topology evolution that doesn't actually change what "healthy" means.

!!! note "Over-specified scenarios are fine"
    Every scenario that passed under strict equality still passes under the relaxed matcher. If your scenarios enumerate every component in `eliminated:` today, they'll keep passing tomorrow — you just don't have to keep the list exhaustive when the model grows.

---

## File location

Place scenarios in a `scenarios/` directory alongside your model:

```
your-project/
├── system.model.yaml
└── scenarios/
    ├── rds-unavailable.yaml
    ├── api-crash-loop.yaml
    └── all-healthy.yaml
```

## Running scenarios

```bash
mgtt simulate --all                        # run all scenarios in scenarios/
mgtt simulate --scenario scenarios/rds-unavailable.yaml  # run one
```

## Example: complete scenario set

### Database failure cascades to API

```yaml
# scenarios/rds-unavailable.yaml
name: rds unavailable
description: >
  rds stops accepting connections. api crash-loops as a result.
  engine should trace the fault to rds, not api.

inject:
  rds:
    available: false
    connection_count: 0
  api:
    ready_replicas: 0
    restart_count: 12
    desired_replicas: 3

expect:
  root_cause: rds
  path: [nginx, api, rds]
  eliminated: [frontend]
```

### Application bug (database healthy)

```yaml
# scenarios/api-crash-loop.yaml
name: api crash-loop independent of rds
description: >
  api crash-loops due to a code error. rds is healthy.

inject:
  api:
    ready_replicas: 0
    restart_count: 24
    desired_replicas: 3
  rds:
    available: true
    connection_count: 120

expect:
  root_cause: api
  path: [nginx, api]
  eliminated: [rds, frontend]
```

### Frontend isolated failure

```yaml
# scenarios/frontend-degraded.yaml
name: frontend crash-looping, api healthy
description: >
  frontend pods are crash-looping. api and rds are healthy.

inject:
  frontend:
    ready_replicas: 0
    restart_count: 8
    desired_replicas: 2
  api:
    ready_replicas: 3
    desired_replicas: 3
    endpoints: 3
  rds:
    available: true
    connection_count: 98

expect:
  root_cause: frontend
  path: [nginx, frontend]
  eliminated: [api, rds]
```

### No false positives

```yaml
# scenarios/all-healthy.yaml
name: all components healthy
description: verifies the engine does not surface false positives.

inject:
  nginx:
    upstream_count: 4
  frontend:
    ready_replicas: 2
    desired_replicas: 2
    endpoints: 2
  api:
    ready_replicas: 3
    desired_replicas: 3
    endpoints: 3
  rds:
    available: true
    connection_count: 87

expect:
  root_cause: none
  eliminated: [nginx, frontend, api, rds]
```

## Common mistake: per-component status assertions

The `expect` block describes the engine's **overall conclusion**, not per-component status. This is wrong:

```yaml
# WRONG — this is not how expect works
expect:
  api:
    status: degraded
  rds:
    status: healthy
  frontend:
    status: healthy
```

The correct format asserts the engine's conclusion — root cause, failure path, and eliminated components:

```yaml
# CORRECT
expect:
  root_cause: api
  path: [nginx, api]
  eliminated: [rds, frontend]
```

The engine determines component states internally from the injected facts. You assert what the engine should *conclude*, not what each component's state should be.

---

## What a failing scenario teaches you

A simulation failure means the engine's conclusion doesn't match your expectation. Common causes:

- **Missing facts in `inject`** — the engine can't determine state without enough facts. Example: injecting `ready_replicas: 0` without `restart_count` makes the engine see `starting` instead of `degraded`.
- **Wrong `expect`** — your assertion doesn't match how the dependency graph actually propagates failures.
- **Model gap** — a missing dependency in `system.model.yaml` means the engine can't trace the failure path.

Each failure reveals something about the model or your understanding of it — at design time, not at 3am.
