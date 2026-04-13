# Simulation

Simulation tests the **model's reasoning**, not the system's behavior. You inject facts, the engine reasons over them, and you assert the conclusion. No running system, no credentials, runs in CI.

```
what it tests:     given these facts, does the engine find the right root cause?
what it doesn't:   whether the system will actually fail this way
scope:             same as a unit test — tests the thing it tests, nothing more
```

---

## The system

Same storefront used in the [troubleshooting walkthrough](troubleshooting.md):

```mermaid
graph LR
  internet([internet]) --> nginx
  nginx[nginx - reverse proxy] --> frontend
  nginx --> api
  frontend[frontend - React SPA] --> api
  api[api - Node.js] --> rds[(rds - AWS RDS)]

  style nginx     fill:#E1F5EE,stroke:#0F6E56,color:#000
  style frontend  fill:#E1F5EE,stroke:#0F6E56,color:#000
  style api       fill:#E1F5EE,stroke:#0F6E56,color:#000
  style rds       fill:#E6F1FB,stroke:#185FA5,color:#000
  style internet  fill:#F1EFE8,stroke:#5F5E5A,color:#000
```

## Step 1 — Write the model

```yaml
# system.model.yaml
meta:
  name: storefront
  version: "1.0"
  providers:
    - kubernetes
  vars:
    namespace: production

components:
  nginx:
    type: ingress
    depends:
      - on: frontend
      - on: api

  frontend:
    type: deployment
    depends:
      - on: api

  api:
    type: deployment
    depends:
      - on: rds

  rds:
    providers:
      - aws
    type: rds_instance
    healthy:
      - connection_count < 500
```

```bash
$ mgtt model validate

  ✓ nginx     2 dependencies valid
  ✓ frontend  1 dependency valid
  ✓ api       1 dependency valid
  ✓ rds       healthy override valid

  4 components · 0 errors · 0 warnings
```

---

## Step 2 — Write failure scenarios

Each scenario injects a set of facts and asserts what the engine should conclude.

### Scenario 1: RDS goes down

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

### Scenario 2: API crash-loops, RDS healthy

```yaml
# scenarios/api-crash-loop.yaml
name: api crash-loop independent of rds
description: >
  api crash-loops due to a code error. rds is healthy.
  engine should find api as root cause and eliminate rds.

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

### Scenario 3: Frontend degraded, API healthy

```yaml
# scenarios/frontend-degraded.yaml
name: frontend crash-looping, api healthy
description: >
  frontend pods are crash-looping. api and rds are healthy.
  engine should find frontend as root cause.

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

### Scenario 4: Everything healthy

```yaml
# scenarios/all-healthy.yaml
name: all components healthy
description: >
  verifies the engine does not surface false positives.

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

---

## Step 3 — Run simulations

```bash
$ mgtt simulate --all

  all components healthy                   ✓ passed
  api crash-loop independent of rds        ✓ passed
  frontend crash-looping, api healthy      ✓ passed
  rds unavailable                          ✓ passed

  4/4 scenarios passed
```

All four pass. The engine correctly:

- Traces rds failure through api to nginx (scenario 1)
- Identifies api as root cause when rds is healthy (scenario 2)
- Finds frontend via the nginx-frontend path (scenario 3)
- Reports no false positives when everything is healthy (scenario 4)

---

## What a failing scenario teaches you

Suppose you wrote scenario 3 without injecting `restart_count` for frontend:

```yaml
inject:
  frontend:
    ready_replicas: 0
    # restart_count missing!
    desired_replicas: 2
```

The simulation would fail — the engine can't determine if frontend is `degraded` (crash-looping) or `starting` (still initializing). Without `restart_count`, the kubernetes provider resolves the state to `starting`, not `degraded`.

This is the engine correctly applying the provider's state definitions:

```
starting:   ready_replicas < desired_replicas  (restart_count not checked)
degraded:   ready_replicas < desired_replicas  AND  restart_count > 5
```

The failure reveals a subtlety: **a deployment that's still starting looks the same as one that's crash-looping until you check restart_count.** You learn this at design time, not at 3am.

The fix: inject `restart_count: 8` to signal crash-looping, or adjust your scenario to test the `starting` state specifically.

---

## Add to CI

```yaml
# .github/workflows/mgtt.yaml
name: model validation

on: [push, pull_request]

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5

      - name: install mgtt
        run: |
          curl -sSL https://raw.githubusercontent.com/sajonaro/mgtt/main/install.sh | sh

      - name: validate model
        run: mgtt model validate

      - name: run scenarios
        run: mgtt simulate --all
```

No credentials. No cluster. Runs on every PR.

If someone edits `system.model.yaml` and removes the `api -> rds` dependency, the `rds-unavailable` scenario fails immediately. The PR is blocked. The blind spot never reaches production.

---

## Design time / runtime duality

The same `system.model.yaml` serves both phases:

| | Design time | Runtime |
|---|---|---|
| Facts source | `scenarios/*.yaml` (injected) | Live probes (kubectl, aws) |
| Command | `mgtt simulate` | `mgtt plan` |
| Needs | Nothing — no credentials, no cluster | Environment credentials |
| Runs in | CI pipeline | On-call engineer's laptop |
| Tests | Model reasoning | Model reasoning + real system |

The model is the architectural decision record. The scenarios are the test suite for the model's reasoning. Together they mean that by the time the system is deployed, the failure detection has already been validated.

See [Troubleshooting](troubleshooting.md) for the runtime side.

---

## Reference

- [Scenario Schema Reference](../reference/scenario-schema.md) — every field in scenario files
- [Type Catalog](../reference/type-catalog.md) — available facts per type (what to inject)
- [Model Schema Reference](../reference/model-schema.md) — every field in `system.model.yaml`
