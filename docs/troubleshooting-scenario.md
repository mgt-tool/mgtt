
### Scenario: Troubleshooting


### Steps
*Done once by whoever knows the system. Not during an incident.*

### 1. Authenticate

MGTT uses credentials already in your environment — nothing to configure separately.

```bash
kubectl config use-context eks-prod-eu   # point at the right cluster
export AWS_PROFILE=prod-readonly         # standard AWS credential chain
```

### 2. Install providers

```bash
$ mgtt provider install kubernetes aws

✓ kubernetes  v1.2.0  auth: kubectl context (eks-prod-eu)  access: read-only
✓ aws         v2.1.0  auth: AWS profile (prod-readonly)    access: read-only
```

### 3. Write the model

```bash
$ mgtt init

✓ created system.model.yaml  — edit to describe your system
```

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
      - aws             # rds is aws-managed, not kubernetes
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

Commit alongside Helm charts and Terraform. Setup is done.

---

### Troubleshooting Scenario: The Incident
*The on-call engineer only needs to press Y.*

Monday 08:14Z. Alert fires: **"503 errors on checkout."**

### 4. Start

```bash
$ mgtt incident start

✓ inc-20240205-0814-001 started
```

### 5. Plan

```bash
$ mgtt plan

  starting from outermost component: nginx
  probing entry point...

  → mgtt probe nginx upstream_count
    cost: low · kubectl read-only

  run? [Y/n] y

  ✓ nginx.upstream_count = 0   ✗ unhealthy
  → nginx has no upstreams — following dependencies inward

  ─────────────────────────────────────────────────────
  2 paths to investigate:

  PATH A  nginx ← api          api not reachable
  PATH B  nginx ← frontend     frontend not reachable

  → mgtt probe api endpoints
    eliminates PATH A if healthy · cost: low · kubectl read-only

  run? [Y/n] y

  ✓ api.endpoints = 0   ✗ unhealthy
  → api has no endpoints — probing further

  → mgtt probe api ready_replicas
    discriminates starting vs degraded · cost: low

  run? [Y/n] y

  ✓ api.ready_replicas = 0    ✗ unhealthy
  ✓ api.desired_replicas = 3
  → api.state: starting (0 of 3 replicas ready)

  → mgtt probe api restart_count
    discriminates cold start vs crash-loop · cost: low

  run? [Y/n] y

  ✓ api.restart_count = 47   ✗ unhealthy
  → api.state: degraded  (crash-looping: 0/3 replicas, 47 restarts)

  api is crash-looping — checking upstream dependency

  → mgtt probe rds available
    rds failure could explain crash-loop · cost: low · AWS read-only

  run? [Y/n] y

  ✓ rds.available = true
  ✓ rds.connection_count = 498
  → rds healthy · PATH B (via rds) eliminated

  ─────────────────────────────────────────────────────
  root cause identified

  ✗ api      crash-looping   replicas=0/3  restarts=47
  ✓ rds      healthy         eliminated
  ✓ frontend not reached     eliminated (api explains symptom first)

  next steps:
    kubectl logs deploy/api -n production --previous
    kubectl describe deploy api -n production
```

### 6. Check the logs, record findings

```bash
$ kubectl logs deploy/api -n production --previous | tail -3
Error: Cannot find module './config/feature-flags'

$ mgtt fact add api startup_error "missing module: ./config/feature-flags" \
      --note "kubectl logs --previous"

$ mgtt fact add api last_deploy_at "2024-02-05T07:50:00Z" \
      --note "deploy 24min before incident"
```

### 7. Close

```bash
$ mgtt incident end

  inc-20240205-0814-001   duration: 14 minutes

  ✗ api     crash-looping
            startup_error: missing module ./config/feature-flags
            last_deploy:   07:50Z  (24min before incident)
  ✓ rds     healthy · eliminated
  ✓ frontend not involved · eliminated

  probes: 5 · facts: 8 · paths: 3 resolved

✓ closed · state file retained: ./inc-20240205-0814-001.state.yaml
```

The state file is the postmortem by construction — timestamped, attributed,
structured. No separate write-up needed for the facts.

---

## What the engineer did

```
mgtt incident start
mgtt plan
y · y · y · y · y
mgtt fact add  (×2, manual observations)
mgtt incident end
```

**14 minutes. 5 probes. Root cause. No system knowledge required at incident time.**

---

## Entry points

The example above uses `mgtt plan` with no arguments — the default, which
starts from the outermost component and works inward. Two alternatives:

```bash
# you already know which component is broken
mgtt plan --component api

# an alert fired with a known value — tree pre-pruned before first probe
mgtt fact add api error_rate 0.94 --collector datadog
mgtt plan --component api
```

---

## Before the incident

The model and failure scenarios can be validated before the system is deployed.
See `SIMULATE.md` for the design-time workflow — writing scenarios, running
them in CI, and what failing scenarios reveal about the model.

The same `system.model.yaml` serves both phases. The scenarios written at
design time are the regression tests that prevent model gaps from becoming
incident blind spots.