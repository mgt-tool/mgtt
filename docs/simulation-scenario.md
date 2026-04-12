# MGTT — Simulation Example


This example shows how MGTT provides value **before the system is deployed**.
The engineer writes the model, defines failure scenarios, and validates that
the constraint engine reasons correctly — in CI, with no running system.


## What Simulation Is and Isn't

Simulation tests the **model's reasoning**, not the **system's behaviour**.

```
what it tests:    given these facts, does the engine find the right root cause?
what it doesn't:  whether or how the system will actually fail
scope:            identical to a unit test — tests the thing it tests, nothing more
```

The `while` and `healthy` conditions (the invariants) are validated statically
by `mgtt model validate`. Simulation validates the traversal on top of that —
does the wiring produce the right conclusions?

A passing simulation is not a guarantee of detection. Novel failures,
unpredicted combinations, and things nobody modelled are outside its scope.
So are unit and integration tests. That's fine.

---

## Step 1 — The Model

The model is written before the system is deployed. This is where the
design-time value starts — writing it forces explicit decisions about
dependencies and failure propagation.

```yaml
# system.model.yaml

meta:
  name:    storefront
  version: 1.0
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

## Step 2 — Write Failure Scenarios

The engineer thinks through the failure modes that matter. For each one,
write a scenario that injects realistic facts and asserts what the engine
should conclude.

### Scenario 1: RDS goes down

```yaml
# scenarios/rds-unavailable.yaml

name:        rds unavailable
description: >
  rds stops accepting connections.
  api starts crash-looping as a result.
  engine should trace the fault to rds, not api.

inject:
  rds:
    available:        false
    connection_count: 0
  api:
    ready_replicas:   0
    restart_count:    12
    desired_replicas: 3

expect:
  root_cause: rds
  path:       [nginx, api, rds]
  eliminated: [frontend]
```

### Scenario 2: API crash-loops, RDS healthy

```yaml
# scenarios/api-crash-loop.yaml

name:        api crash-loop independent of rds
description: >
  api crash-loops due to a code error — not a database problem.
  rds is healthy. engine should find api as root cause
  and correctly eliminate rds as a candidate.

inject:
  api:
    ready_replicas:   0
    restart_count:    24
    desired_replicas: 3
  rds:
    available:        true
    connection_count: 120

expect:
  root_cause: api
  path:       [nginx, api]
  eliminated: [rds, frontend]
```

### Scenario 3: Frontend degraded, API healthy

```yaml
# scenarios/frontend-degraded.yaml

name:        frontend crash-looping, api healthy
description: >
  frontend pods are crash-looping. api and rds are healthy.
  engine should find frontend as root cause via the nginx → frontend path.

inject:
  frontend:
    ready_replicas:   0
    restart_count:    8
    desired_replicas: 2
  api:
    ready_replicas:   3
    desired_replicas: 3
    endpoints:        3
  rds:
    available:        true
    connection_count: 98

expect:
  root_cause: frontend
  path:       [nginx, frontend]
  eliminated: [api, rds]
```

### Scenario 4: Everything healthy

```yaml
# scenarios/all-healthy.yaml

name:        all components healthy
description: >
  verifies that the engine does not surface false positives
  when everything is operating normally.

inject:
  nginx:
    upstream_count: 4
  frontend:
    ready_replicas:   2
    desired_replicas: 2
    endpoints:        2
  api:
    ready_replicas:   3
    desired_replicas: 3
    endpoints:        3
  rds:
    available:        true
    connection_count: 87

expect:
  root_cause: none
  eliminated: [nginx, frontend, api, rds]
```

---

## Step 3 — Run Simulations

```bash
$ mgtt simulate --all

  rds-unavailable        ✓ passed
  api-crash-loop         ✓ passed
  frontend-degraded      ✗ failed
  all-healthy            ✓ passed

  3/4 scenarios passed
```

One failure. Let's look at it:

```bash
$ mgtt simulate --scenario scenarios/frontend-degraded.yaml

  scenario  frontend degraded, api healthy
  mode      simulation — no real system contacted

  injecting facts:
    frontend.ready_replicas   = 0
    frontend.restart_count    = 8
    frontend.desired_replicas = 2
    api.ready_replicas        = 3
    api.desired_replicas      = 3
    api.endpoints             = 3
    rds.available             = true
    rds.connection_count      = 98

  constraint engine result:

  ✗ root_cause  no root cause found   (expected: frontend)
  ✗ path        not resolved          (expected: nginx ← frontend)
  ✓ eliminated  api, rds              (expected)

  scenario failed

  reason:
    frontend.state could not be resolved from injected facts.
    the kubernetes/deployment provider requires restart_count
    to determine degraded state, but restart_count was not
    injected — ready_replicas=0 alone resolves to 'starting',
    not 'degraded'. the nginx ← frontend path was not activated.

  suggestion:
    either inject restart_count for frontend, or check that
    the 'starting' state is also an expected path activator
    for this scenario.
```

The engine is telling us something real: the model correctly distinguishes
between a deployment that's still starting (ready_replicas=0, restart_count=0)
and one that's crash-looping (ready_replicas=0, restart_count>5). The scenario
was underspecified — it forgot to inject restart_count.

Fix the scenario:

```yaml
# scenarios/frontend-degraded.yaml (fixed)

inject:
  frontend:
    ready_replicas:   0
    restart_count:    8      # ← added — signals degraded not starting
    desired_replicas: 2
  ...
```

```bash
$ mgtt simulate --scenario scenarios/frontend-degraded.yaml

  ✓ root_cause  frontend  (expected: frontend)
  ✓ path        nginx ← frontend  (expected)
  ✓ eliminated  api, rds  (expected)

  scenario passed
```

---

## Step 4 — What the Failure Revealed

The simulation failure was not a bug in the engine. It was the engine
correctly applying the provider's state definition:

```
starting:   ready_replicas < desired_replicas  (and restart_count not high)
degraded:   ready_replicas < desired_replicas  AND restart_count > 5
```

Writing the scenario exposed a subtlety in the model that the engineer
hadn't thought through: **a deployment that's still starting looks the same
as one that's crash-looping until you check restart_count.**

This is design-time value. The engineer now knows:

1. During a real incident, `mgtt plan` will probe `restart_count` to
   discriminate between these two states — which is exactly what we saw
   in the troubleshooting example.

2. If restart_count is uncollectable for some reason, the engine will
   correctly flag `starting` as an unresolved state rather than guessing.

3. The `while` condition on the nginx → frontend dependency is correctly
   activated by `degraded` but not by `starting` — which means during a
   deployment rollout, nginx won't be flagged as potentially unhealthy
   just because frontend pods are initialising.

None of this required a running system. It came from writing scenarios and
reading what the engine told us.

---

## Step 5 — Add to CI

```yaml
# .github/workflows/mgtt.yaml

name: mgtt model validation

on: [push, pull_request]

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: install mgtt
        run: curl -sSL https://mgtt.dev/install.sh | sh

      - name: install providers
        run: mgtt provider install kubernetes aws

      - name: validate model
        run: mgtt model validate

      - name: run scenarios
        run: mgtt simulate --all
```

No credentials. No cluster. No running system. This runs on every PR.

If someone edits `system.model.yaml` and accidentally removes the
`api → rds` dependency, the `rds-unavailable` scenario fails immediately:

```
$ mgtt simulate --all

  rds-unavailable        ✗ failed
    expected path: [nginx, api, rds]
    got:           [nginx, api]  — rds not reachable from api
    reason:        api has no dependency on rds in model

  api-crash-loop         ✓ passed
  frontend-degraded      ✓ passed
  all-healthy            ✓ passed
```

The PR is blocked. The dependency is restored. The blind spot never reaches
production.

---

## The Design-Time / Runtime Duality

The same `system.model.yaml` serves both phases:

```
design time                    runtime
──────────────────────────────────────────────────────
system.model.yaml              system.model.yaml (same file)
scenarios/*.yaml               system.state.yaml
mgtt simulate                  mgtt plan
no credentials needed          credentials from environment
CI pipeline                    on-call engineer
tests reasoning                tests reasoning + observes reality
```

The model is the architectural decision record. The scenarios are the
test suite for the model's reasoning. Together they mean that by the time
the system is deployed, the failure detection has already been validated.