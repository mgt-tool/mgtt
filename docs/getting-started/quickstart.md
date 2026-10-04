# Quick start

This page walks the full loop on a four-component storefront: model, simulate, diagnose, retrospective.

## 1. Install mgtt and providers

```bash
curl -sSL https://raw.githubusercontent.com/mgt-tool/mgtt/main/install.sh | sh
mgtt provider install kubernetes aws
```

Other install routes: `go install github.com/mgt-tool/mgtt/cmd/mgtt@v{{ MGTT_VERSION }}`, or the image `ghcr.io/mgt-tool/mgtt:{{ MGTT_VERSION }}`. Pin `X.Y.Z` in CI. The installer verifies checksums and honours `MGTT_VERSION` and `INSTALL_DIR`.

## 2. Model the system

`mgtt init` scaffolds `system.model.yaml`. Then edit it:

```yaml
meta:
  name: storefront
  version: "1.0"
  providers:
    - mgt-tool/kubernetes@>=3.0.0
    - mgt-tool/aws@>=1.0.0
  vars:
    namespace: production        # substituted into probe commands

components:
  nginx:
    type: kubernetes.deployment
    depends:
      - on: frontend
      - on: api
  frontend:
    type: kubernetes.deployment
    depends:
      - on: api
  api:
    type: kubernetes.deployment
    depends:
      - on: rds
  rds:
    type: aws.rds_instance
    resource: shop-prod-db       # the real resource name, if it differs from the key
```

Each type brings its facts (`ready_replicas`, `available`, …), its states (`crashed`, `stopped`, …), and default health rules. To list them, run `mgtt provider inspect kubernetes deployment`.

```
$ mgtt model validate
  ✓ nginx     2 dependencies valid
  ✓ frontend  1 dependency valid
  ✓ api       1 dependency valid
  ✓ rds       no dependencies

  4 components · 0 errors · 0 warnings

$ mgtt visualize        # writes model-graph.md, a Mermaid diagram of the model
```

## 3. Write scenarios

A scenario injects facts and states what the engine must conclude. Put scenarios in `scenarios/`:

```yaml
# scenarios/rds-down.yaml
name: rds down
description: rds stops; api crash-loops because of it. Blame rds, not api.
inject:
  rds: { available: false }
  api: { ready_replicas: 0, desired_replicas: 3, restart_count: 12 }
expect:
  root_cause: rds
  path: [nginx, api, rds]
  eliminated: [frontend]
```

```yaml
# scenarios/api-crash.yaml
name: api crash-loop, rds healthy
inject:
  api: { ready_replicas: 0, desired_replicas: 3, restart_count: 24 }
  rds: { available: true, connection_count: 120 }
expect:
  root_cause: api
  eliminated: [rds, frontend]
```

Add an all-healthy scenario with `root_cause: none`, so you also catch false alarms.

## 4. Simulate, and wire it into CI

```
$ mgtt simulate --all
  all healthy                              ✓ passed
  api crash-loop, rds healthy              ✓ passed
  rds down                                 ✓ passed

  3/3 scenarios passed
```

Now delete `api`'s dependency on `rds` and run it again:

```
  api crash-loop, rds healthy              ✗ FAILED
    expected: root_cause=api path=[nginx, api] eliminated=[rds, frontend]
    actual:   root_cause=api path=[nginx, api] eliminated=[frontend]
  rds down                                 ✗ FAILED
    expected: root_cause=rds path=[nginx, api, rds] eliminated=[frontend]
    actual:   root_cause=api path=[nginx, api] eliminated=[frontend]
```

Without the edge, the engine blames api for a database outage. CI is where that mistake should surface:

```yaml
# .github/workflows/mgtt.yaml
on: [push, pull_request]
jobs:
  model:
    runs-on: ubuntu-latest
    container: ghcr.io/mgt-tool/mgtt:{{ MGTT_VERSION }}
    steps:
      - uses: actions/checkout@v5
      - run: mgtt model validate
      - run: mgtt simulate --all
```

Next, generate the failure-chain index that `diagnose` uses. Commit it; `model validate` fails if it falls out of date:

```
$ mgtt model validate --write-scenarios
  wrote 710 scenarios to scenarios.yaml
```

## 5. Diagnose

When an alert fires:

```
$ mgtt incident start
  ✓ inc-20261004-0814-001 started

$ mgtt diagnose --suspect api
Root cause: rds
Scenario:   rds.stopped → api.crashed → nginx.degraded
Probes run: 7/20   Time: 3.2s/5m0s
Hint:       suspect=api — appeared mid-chain; real root was rds
Trail:
  1. api.restart_count — api.restart_count = 47
  2. rds.available — rds.available = false
  …
```

Probes run with your own credentials, read-only by default. `--suspect` is a hint, not a filter. `mgtt plan` runs the same loop and asks before each probe; `mgtt fact add api error_rate 0.94` records something you saw yourself. To have an AI agent run this step, see [AI agents](../guides/agents.md).

## 6. Retrospective

```
$ mgtt incident end --emit-scenario --suggest-scenarios
wrote scenarios/inc-20261004-0814-001.yaml — passes; commit it to keep this diagnosis under test
```

- **`--emit-scenario`** turns every fact observed during the incident into a scenario, with the engine's conclusion as `expect`. Review it, give it a name, and commit it.
- **`--suggest-scenarios`** checks whether the incident followed a failure chain your model never predicted. If it did, it writes `.mgtt/pending-scenarios/<id>.patch` describing the missing chain. To accept the chain, add the propagation to the model (`failure_modes:`) and regenerate `scenarios.yaml`.

The incident record stays in `<incident-id>.state.yaml` in the working directory.

## Next

- [How it works](../concepts/how-it-works.md): what the engine does with all this
- [Model and scenario reference](../reference/model.md)
- [Providers](../guides/providers.md)
