# Blue/green storefront on EKS

A complete, non-trivial mgtt walkthrough based on a real production e-commerce platform. PHP-FPM behind nginx, blue/green traffic switching, an async queue tier, scheduled cron, and an AWS-managed data layer (RDS + ElastiCache + AmazonMQ + S3 + CloudFront).

This page is a walkthrough, not a reference. It shows how a working engineer reaches the model you see here — including the places where the first attempt was wrong and had to be refined.

## On this page

1. [The system](#the-system)
2. [The model](#the-model)
3. [Validate](#validate)
4. [Scenarios](#scenarios)
5. [Simulate in CI](#simulate-in-ci)
6. [At 3am: `mgtt diagnose`](#at-3am-mgtt-diagnose)
7. [Lessons from the first few weeks](#lessons-from-the-first-few-weeks)

---

## The system

`acme-shop` is a storefront serving `shop.acme.example`. Deployed on EKS with blue/green traffic switching: two identical deployment sets (`color: blue`, `color: green`) run side-by-side, and a single `Service` (`acme-shop-svc`) points its selector at whichever color is live.

![blue/green storefront model](../images/blue-green-storefront.svg)

<!-- Source: docs/images/blue-green-storefront.d2 — regenerate with `d2 docs/images/blue-green-storefront.d2 docs/images/blue-green-storefront.svg` -->


The stateless tiers (nginx, php-fpm, consumer) come in blue and green pairs. The **shared data layer** (RDS, Redis, MQ, OpenSearch, S3) is color-agnostic — both colors connect identically. Cron is a singleton; it reads the env config from the same Secret the active color uses.

This is the serving path only. Cluster operators (ArgoCD, Karpenter, ALB Controller), the observability stack (Alloy/Loki/Prometheus/Grafana), and the blue/green direct-QA ingresses are deliberately outside the model — their failures don't directly break HTTP serving, and adding them would multiply enumerated scenarios without diagnostic value.

---

## The model

`system.model.yaml`:

```yaml
meta:
  name: acme-shop
  version: "1.0"
  # Providers supply the types (cdn, deployment, rds_instance, ...) and
  # the probes that populate facts. This system runs on EKS (kubernetes)
  # with its data layer on AWS managed services, so both are needed.
  providers:
    - mgt-tool/kubernetes@^3.0.0
    - mgt-tool/aws@^1.0.0
  # Model-wide variables. Substituted into probe arguments via {name}
  # placeholders (e.g. `resource: acme-shop-{env}-rds`). A component
  # can override any of these with its own `vars:` block — see
  # `external-secrets` below.
  vars:
    namespace: default
    cluster: acme-shop-stage
    region: eu-central-1
    env: stage          # flip to `prod` on the prod branch
  scenarios: none       # see note below on enumerated scenarios

components:

  # ─── Edge / public entry ────────────────────────────────────────
  # Kubernetes-owned types (cdn, ingress, service, deployment, operator)
  # don't need a per-component `providers:` line — mgtt resolves them
  # by type lookup against meta.providers.
  cloudflare:
    type: cdn
    depends:
      - on: acme-shop-ingress

  acme-shop-ingress:
    type: ingress
    depends:
      - on: acme-shop-svc

  # ─── Service layer (color-selecting) ────────────────────────────
  acme-shop-svc:
    type: service
    depends:
      - on: acme-shop-nginx-blue
      - on: acme-shop-nginx-green

  # ─── nginx tier (per color) ─────────────────────────────────────
  acme-shop-nginx-blue:
    type: deployment
    depends:
      - on: acme-shop-php-fpm-blue

  acme-shop-nginx-green:
    type: deployment
    depends:
      - on: acme-shop-php-fpm-green

  # ─── php-fpm tier (per color) ───────────────────────────────────
  # All backend work lives here; depends on the full shared data
  # layer. The app-config Secret selects which color's env is live.
  acme-shop-php-fpm-blue:
    type: deployment
    depends:
      - on: external-secrets
      - on: rds
      - on: redis
      - on: mq
      - on: opensearch
      - on: media-bucket

  acme-shop-php-fpm-green:
    type: deployment
    depends:
      - on: external-secrets
      - on: rds
      - on: redis
      - on: mq
      - on: opensearch
      - on: media-bucket

  # ─── Async workers (per color) ──────────────────────────────────
  # `depends: [{on: x}, ...]` is inline YAML — same structure as the
  # multi-line `- on: x` form above. Pick whichever reads cleaner.
  acme-shop-consumer-blue:
    type: deployment
    depends: [{on: mq}, {on: rds}, {on: redis}, {on: external-secrets}]

  acme-shop-consumer-green:
    type: deployment
    depends: [{on: mq}, {on: rds}, {on: redis}, {on: external-secrets}]

  # ─── Cron (singleton, color-agnostic) ───────────────────────────
  acme-shop-cron:
    type: deployment
    depends: [{on: external-secrets}, {on: rds}, {on: redis}, {on: mq}]

  scheduled_jobs:
    type: business_process
    depends: [{on: acme-shop-cron}]

  async_jobs:
    type: business_process
    depends: [{on: acme-shop-consumer-blue}, {on: acme-shop-consumer-green}, {on: mq}]

  # ─── In-cluster search ──────────────────────────────────────────
  opensearch:
    type: deployment
    # `healthy:` is a list of boolean rules. Each has the form
    # `[<component>.]<fact> <op> <value>` with `<op>` in
    # `== != < > <= >=`. The `<component>.` prefix is optional —
    # omit it to reference this component's own fact (what we're
    # doing with `ready_replicas` here), or include it to reach
    # across (e.g. `redis.available == true`). Fact names come from
    # the type's probes — `deployment` is owned by the kubernetes
    # provider. All rules must hold for the component to count as
    # healthy; missing facts are treated as "don't know" and skipped
    # rather than failed. A component-level `healthy:` REPLACES the
    # type default outright (it does not merge) — see the override
    # notes below for why we override for opensearch, rds, redis, mq.
    healthy:
      - ready_replicas >= 1

  # ─── AWS managed data layer ─────────────────────────────────────
  # AWS types use short readable keys (`rds`, `redis`, `mq`) while
  # `resource:` carries the real AWS identifier. Per-component
  # `providers:` pins which provider resolves the type. `{env}`
  # expands from meta.vars, so the same file ships unchanged across
  # stage and prod — only the branch-specific `vars.env` flips.
  rds:
    type: rds_instance
    providers: [mgt-tool/aws@^1.0.0]
    resource: acme-shop-{env}-rds
    healthy:
      - connection_count < 500

  redis:
    type: elasticache_cluster
    providers: [mgt-tool/aws@^1.0.0]
    resource: acme-shop-{env}-redis
    healthy:
      - available == true

  mq:
    type: mq_broker
    providers: [mgt-tool/aws@^1.0.0]
    resource: acme-shop-{env}-mq
    healthy:
      - queue_depth < 10000

  media-bucket:
    type: s3_bucket
    providers: [mgt-tool/aws@^1.0.0]
    resource: acme-shop-stage-media-8f3e9c

  cloudfront:
    type: cloudfront_distribution
    # Strictly redundant — `cloudfront_distribution` only lives in the
    # aws provider, so mgtt would resolve it from meta.providers without
    # this line. Kept for self-documentation: the aws-vs-k8s split stays
    # obvious at a glance, and the version pin survives even if a future
    # provider happens to introduce a clashing type name.
    providers: [mgt-tool/aws@^1.0.0]
    resource: EABCDEF123456X
    depends:
      - on: media-bucket

  # ─── Config / secrets sources ───────────────────────────────────
  ssm-app-config:
    type: ssm_parameter
    providers: [mgt-tool/aws@^1.0.0]
    resource: /platform/defaults/app-config

  external-secrets:
    type: operator
    # Component-scoped `vars:` override `meta.vars:` for this component
    # only — probes against ESO run in the `external-secrets` namespace
    # instead of the model-wide `default`.
    vars:
      namespace: external-secrets
    depends:
      - on: ssm-app-config
```

### Why it looks like this

**Kubernetes components use the real resource name as the mgtt key** (`acme-shop-nginx-blue`), because the kubernetes provider's `kubectl get <type> <key>` probes resolve it directly in `default`. AWS components use readable short keys (`rds`, `redis`, `mq`) with `resource:` carrying the real AWS identifier.

**`resource: acme-shop-{env}-rds`** — templating from `meta.vars.env` means this same file ships unchanged for stage and prod; only the branch-specific `vars.env` flips.

**Random suffixes are hardcoded.** The S3 bucket `acme-shop-stage-media-8f3e9c` doesn't follow the `{env}` pattern (random bucket suffix). Same for CloudFront distribution IDs and SSM paths. Readable key + hardcoded resource is fine — the decoupling still holds.

**`scenarios: none`** on the model. The enumerated `scenarios.yaml` for this system is tens of thousands of chains — ~40MB. Too chunky for git history. Regenerate locally with `mgtt model validate --write-scenarios`, gitignore the result, and skip the drift check on this model. Hand-authored scenarios stay in `scenarios/` and run in CI via `mgtt simulate --all`.

**nginx → php-fpm is a deployment edge, not a service edge.** The `acme-shop-php-fpm-{blue,green}` Services exist in the cluster but share the Deployment's exact name — mgtt requires unique component keys, so the model walks to the deployment directly. The Deployment's `ready_replicas` / `restart_count` are what actually diagnose php-fpm health; the svc-level probe wouldn't have added signal.

**`healthy:` overrides replace the type default, they don't merge.** Three cases where that matters:

- `opensearch` — the default `deployment` type has many rules; for a single-replica staging deployment, the full set over-constrains. `ready_replicas >= 1` is the whole signal.
- `redis` — the `elasticache_cluster` default includes `cache_hit_ratio > 80`, which trips on idle stage (ratio is 0 when nothing reads). The component-level `available == true` replaces the whole rule set. A real Redis outage still flips `available` to false.
- `mq` — dropped `consumer_count > 0` from the default. On stage, consumers are scaled to 0 and never attach to the broker, so the rule flagged idle-by-design as an incident. `queue_depth < 10000` is the real safety bound: a backed-up queue is the symptom regardless of how many consumers are attached.

**Business-process components** (`scheduled_jobs`, `async_jobs`) give the engine a user-visible symptom layer for chains rooted at cron or mq. Treating them as generic components with operator-observable facts (are jobs processed on time, is the queue draining) gives mgtt a terminal to reason from.

**External Secrets Operator is modelled, not the Secret it produces.** The operator's health is the real signal — a healthy operator implies a fresh Secret. And the view-only IAM policy used by the CI diagnose role excludes `secrets`, so probing the Secret directly would 403. The operator is probed with `deployment_ready` only (not `crd_registered` or `webhook_*`), because cluster-scoped CRD gets also aren't in the view policy, and this ESO install ships no webhook configuration — setting the vars for probes that would come back forbidden just adds noise.

**The operator lives in its own namespace.** `vars.namespace: external-secrets` on that single component overrides the model-wide `meta.vars.namespace: default`, so the `kubectl -n` argument in the probe resolves correctly.

---

## Validate

```
$ mgtt model validate

  ✓ cloudflare                  1 dependency valid
  ✓ acme-shop-ingress           1 dependency valid
  ✓ acme-shop-svc               2 dependencies valid
  ✓ acme-shop-nginx-blue        1 dependency valid
  ✓ acme-shop-nginx-green       1 dependency valid
  ✓ acme-shop-php-fpm-blue      6 dependencies valid
  ✓ acme-shop-php-fpm-green     6 dependencies valid
  ✓ acme-shop-consumer-blue     4 dependencies valid
  ✓ acme-shop-consumer-green    4 dependencies valid
  ✓ acme-shop-cron              4 dependencies valid
  ✓ scheduled_jobs              1 dependency valid
  ✓ async_jobs                  3 dependencies valid
  ✓ opensearch                  healthy override valid
  ✓ rds                         healthy override valid
  ✓ redis                       healthy override valid
  ✓ mq                          healthy override valid
  ✓ media-bucket                resolved
  ✓ cloudfront                  1 dependency valid
  ✓ ssm-app-config              resolved
  ✓ external-secrets            1 dependency valid

  20 components · 0 errors · 0 warnings
```

At this point the model is syntactically correct and every type/fact resolves against the declared providers. Nothing has been said yet about whether it reasons *well* — that's what scenarios are for.

---

## Scenarios

Five scenarios live in `scenarios/`, each testing a different lesson the engine has to get right.

### 1. All healthy — no false positives

`scenarios/all-healthy.yaml` injects healthy facts for every component and asserts `root_cause: none`. The moment this fails, you've added a rule that trips on a healthy system — the `consumer_count > 0` mistake gets caught here.

```yaml
name: all components healthy

inject:
  acme-shop-nginx-blue:     { ready_replicas: 2, desired_replicas: 2, condition_available: true, restart_count: 0 }
  acme-shop-nginx-green:    { ready_replicas: 2, desired_replicas: 2, condition_available: true, restart_count: 0 }
  acme-shop-php-fpm-blue:   { ready_replicas: 3, desired_replicas: 3, condition_available: true, restart_count: 0 }
  acme-shop-php-fpm-green:  { ready_replicas: 3, desired_replicas: 3, condition_available: true, restart_count: 0 }
  acme-shop-consumer-blue:  { ready_replicas: 1, desired_replicas: 1, condition_available: true, restart_count: 0 }
  acme-shop-consumer-green: { ready_replicas: 1, desired_replicas: 1, condition_available: true, restart_count: 0 }
  acme-shop-cron:           { ready_replicas: 1, desired_replicas: 1, condition_available: true, restart_count: 0 }
  opensearch:               { ready_replicas: 1, desired_replicas: 1, condition_available: true, restart_count: 0 }
  rds:                      { available: true, connection_count: 120 }
  redis:                    { available: true, connection_count: 200, cache_hit_ratio: 0.95 }
  mq:                       { available: true, consumer_count: 22, queue_depth: 34 }
  media-bucket:             { accessible: true }
  cloudfront:               { enabled: true, deployed: true }
  external-secrets:         { deployment_ready: true, crd_registered: true }

expect:
  root_cause: none
  eliminated:
    - cloudflare
    - external-secrets
    - acme-shop-ingress
    - acme-shop-nginx-blue
    - acme-shop-nginx-green
    - acme-shop-php-fpm-blue
    - acme-shop-php-fpm-green
    - acme-shop-svc
    - mq
    - opensearch
    - rds
    - redis
    - media-bucket
    - ssm-app-config
```

### 2. Blue nginx crash-loop, green healthy

A bad image was rolled to blue nginx. Blue crash-loops, green is fine. Traffic is currently on blue, so the live path is broken — runbook here is "flip the service selector to green".

The engine sees `acme-shop-nginx-blue` degraded AND `acme-shop-nginx-green` healthy. Because `acme-shop-svc` hard-depends on both, mgtt flags `acme-shop-nginx-blue` as the root cause of the live-traffic symptom — and `acme-shop-nginx-green` gets eliminated, confirming the failover target is actually available.

```yaml
name: blue nginx crash-looping, green healthy

inject:
  acme-shop-nginx-blue:     { ready_replicas: 0, desired_replicas: 2, condition_available: false, restart_count: 18 }
  acme-shop-nginx-green:    { ready_replicas: 2, desired_replicas: 2, condition_available: true,  restart_count: 0 }
  acme-shop-svc:            { endpoint_count: 0, selector_match: false }
  acme-shop-php-fpm-blue:   { ready_replicas: 3, desired_replicas: 3, condition_available: true,  restart_count: 0 }
  acme-shop-php-fpm-green:  { ready_replicas: 3, desired_replicas: 3, condition_available: true,  restart_count: 0 }
  rds:                      { available: true, connection_count: 140 }
  redis:                    { available: true, connection_count: 200, cache_hit_ratio: 0.94 }
  mq:                       { available: true, consumer_count: 22, queue_depth: 40 }
  opensearch:               { ready_replicas: 1, desired_replicas: 1, condition_available: true, restart_count: 0 }
  media-bucket:             { accessible: true }
  external-secrets:         { deployment_ready: true, crd_registered: true }

expect:
  root_cause: acme-shop-nginx-blue
  path: [cloudflare, acme-shop-ingress, acme-shop-svc, acme-shop-nginx-blue]
  eliminated: [external-secrets, acme-shop-nginx-green, acme-shop-php-fpm-blue, acme-shop-php-fpm-green, mq, opensearch, rds, redis, media-bucket, ssm-app-config]
```

### 3. RDS unavailable — dramatic cascade

RDS stops accepting connections. php-fpm (both colors) can't load config or run catalog queries, so pods crash-loop. nginx returns 5xx. Cron and consumers are equally affected. The failure is splattered across half the dependency graph — the engine must trace it to `rds`, not blame a color.

```yaml
name: rds unavailable

inject:
  rds:                      { available: false, connection_count: 0 }
  redis:                    { available: true,  connection_count: 210, cache_hit_ratio: 0.82 }
  mq:                       { available: true,  consumer_count: 0, queue_depth: 4200 }
  opensearch:               { ready_replicas: 1, desired_replicas: 1, condition_available: true, restart_count: 0 }
  media-bucket:             { accessible: true }
  external-secrets:         { deployment_ready: true, crd_registered: true }
  acme-shop-php-fpm-blue:   { ready_replicas: 0, desired_replicas: 3, condition_available: false, restart_count: 14 }
  acme-shop-php-fpm-green:  { ready_replicas: 0, desired_replicas: 3, condition_available: false, restart_count: 12 }
  acme-shop-consumer-blue:  { ready_replicas: 0, desired_replicas: 1, condition_available: false, restart_count: 8 }
  acme-shop-consumer-green: { ready_replicas: 0, desired_replicas: 1, condition_available: false, restart_count: 9 }

expect:
  root_cause: rds
  path: [cloudflare, acme-shop-ingress, acme-shop-svc, acme-shop-nginx-blue, acme-shop-php-fpm-blue, rds]
  eliminated: [external-secrets, mq, opensearch, redis, media-bucket, ssm-app-config]
```

### 4. Redis unavailable — observability trap

This scenario tests whether the engine gets fooled. Redis goes down; sessions evict; cache hit ratio collapses. php-fpm pods stay up (they don't *require* Redis to start), but every request now hits the database — RDS connection count climbs to 480 (just under the 500 limit). If you probed naively, RDS would look *stressed*.

The engine has to recognise that `redis.available == false` is the actual failed predicate, and RDS is still within its `healthy:` bound. Root cause: `redis`.

```yaml
name: redis unavailable

inject:
  redis:                    { available: false, connection_count: 0,   cache_hit_ratio: 0.0 }
  rds:                      { available: true,  connection_count: 480 }   # stressed but within bound
  mq:                       { available: true,  consumer_count: 22,  queue_depth: 38 }
  opensearch:               { ready_replicas: 1, desired_replicas: 1, condition_available: true, restart_count: 0 }
  media-bucket:             { accessible: true }
  external-secrets:         { deployment_ready: true, crd_registered: true }
  acme-shop-php-fpm-blue:   { ready_replicas: 3, desired_replicas: 3, condition_available: true, restart_count: 1 }
  acme-shop-php-fpm-green:  { ready_replicas: 3, desired_replicas: 3, condition_available: true, restart_count: 1 }

expect:
  root_cause: redis
  path: [cloudflare, acme-shop-ingress, acme-shop-svc, acme-shop-nginx-blue, acme-shop-php-fpm-blue, redis]
  eliminated: [external-secrets, acme-shop-nginx-green, acme-shop-php-fpm-green, mq, opensearch, rds, media-bucket, ssm-app-config]
```

### 5. External Secrets Operator down — deep chain cut short

ESO's Deployment is unavailable, so the in-cluster app-config Secret can't be reconciled from SSM. php-fpm pods on both colors fail to start (mount error on a stale/missing Secret). Cron fails. Consumers can't start. The SSM parameter itself — source of truth — is fine.

The test is whether the engine cuts the chain at `external-secrets` rather than walking all the way to `ssm-app-config` and blaming it. A probe to SSM returns "ok", eliminating it. A probe to the operator's deployment returns "not ready" — that's the cut point.

```yaml
name: external-secrets operator down

inject:
  ssm-app-config:           { exists: true, version: 7 }
  external-secrets:         { deployment_ready: false, crd_registered: true, restart_count: 0 }
  rds:                      { available: true, connection_count: 100 }
  redis:                    { available: true, connection_count: 200, cache_hit_ratio: 0.93 }
  mq:                       { available: true, consumer_count: 0, queue_depth: 1800 }
  media-bucket:             { accessible: true }
  opensearch:               { ready_replicas: 1, desired_replicas: 1, condition_available: true, restart_count: 0 }
  acme-shop-php-fpm-blue:   { ready_replicas: 0, desired_replicas: 3, condition_available: false, condition_progressing: false, restart_count: 0 }
  acme-shop-php-fpm-green:  { ready_replicas: 0, desired_replicas: 3, condition_available: false, condition_progressing: false, restart_count: 0 }
  acme-shop-cron:           { ready_replicas: 0, desired_replicas: 1, condition_available: false, condition_progressing: false, restart_count: 0 }

expect:
  root_cause: external-secrets
  path: [cloudflare, acme-shop-ingress, acme-shop-svc, acme-shop-nginx-blue, acme-shop-php-fpm-blue, external-secrets]
  eliminated: [mq, opensearch, rds, redis, media-bucket, ssm-app-config]
```

Note `restart_count: 0` on the php-fpm and cron pods — a mount failure stalls the rollout before the container starts, so the pod never crashes. "Not ready, never started" is a different signature from "crash-loop", and the model's facts have to carry that distinction for the engine to cut the chain correctly.

---

## Simulate in CI

```
$ mgtt simulate --all

  all components healthy                        ✓ passed
  blue nginx crash-looping, green healthy       ✓ passed
  rds unavailable                               ✓ passed
  redis unavailable                             ✓ passed
  external-secrets operator down                ✓ passed

  5/5 scenarios passed
```

No cluster, no credentials. This runs on every PR in ~400ms:

```yaml
# .gitlab-ci.yml (or .github/workflows/mgtt.yaml)
validate-model:
  image: ghcr.io/mgt-tool/mgtt:latest
  script:
    - mgtt model validate
    - mgtt simulate --all
```

Every scenario is a test of the model's *reasoning*. If someone renames `rds` to `primary-db` without updating the ingress-tier dependency, scenario 3 fails. If someone re-adds `consumer_count > 0` to the `mq` healthy rule, scenario 1 fails. The scenarios are the guard against silent drift.

---

## At 3am: `mgtt diagnose`

A Cloudflare alert fires: 5xx rate on `shop.acme.example` just crossed 5%. On-call pastes `/diagnose api` into Slack; a GitLab job runs:

```
$ mgtt diagnose --suspect acme-shop-php-fpm-blue --max-probes 10

  ▶ probe cloudflare            enabled           ✓ healthy   ← eliminated
  ▶ probe acme-shop-ingress     address_assigned  ✓ healthy   ← eliminated
  ▶ probe acme-shop-svc         endpoint_count    ✗ 0 endpoints
  ▶ probe acme-shop-nginx-blue  ready_replicas    ✗ 0/2 ready
  ▶ probe acme-shop-php-fpm-blue ready_replicas   ✗ 0/3 ready
  ▶ probe external-secrets      deployment_ready  ✗ unhealthy
  ▶ probe ssm-app-config        exists            ✓ healthy   ← eliminated
  ▶ probe rds                   available         ✓ healthy   ← eliminated
  ▶ probe redis                 available         ✓ healthy   ← eliminated

  Root cause: external-secrets.deployment_unavailable
  Chain:      cloudflare ← acme-shop-ingress ← acme-shop-svc ←
              acme-shop-nginx-blue ← acme-shop-php-fpm-blue ← external-secrets
  Eliminated: cloudflare, acme-shop-ingress, ssm-app-config, rds, redis
  Probes run: 9/10
  Partial visibility: none
```

Nine probes, five branches eliminated, one chain confirmed. The runbook link (not shown) points to `docs/runbooks/external-secrets-recovery.md`. On-call knows where to look before they've opened a terminal.

The reason diagnose runs this fast is the **committed `scenarios.yaml`**: at design time, `mgtt model validate --write-scenarios` enumerated every plausible failure chain this model can produce, so diagnose prunes whole branches (media-bucket, cloudfront, opensearch, mq, async_jobs, consumer/cron) with a single group-elimination step before the first live probe runs.

---

## Lessons from the first few weeks

Real usage of mgtt is a dialogue between the model and the system. These four refinements came from running against staging for a week before the model was trusted:

**`consumer_count > 0` on the mq_broker tripped on idle stage.** Staging often has consumer deployments at zero replicas for cost. The default `mq_broker` healthy rule flagged this as an incident on every run. The fix: component-level `healthy:` that *replaces* the default with `queue_depth < 10000`, because queue depth is the symptom an operator actually cares about regardless of consumer count. Scenario 1 (`all-healthy`) is the regression test.

**`cache_hit_ratio > 80` on idle Redis.** Same shape: on a freshly-started stage, the hit ratio is 0 because nothing's reading. The default elasticache_cluster rule flagged Redis as unhealthy while Redis was fine. Replaced with `available == true`.

**Modelling the Secret instead of the operator.** The first attempt had a `kubernetes.secret` component `app-config-active` as the dependency of php-fpm. But (a) the CI diagnose role uses a view-only IAM policy that excludes `secrets` — so the probe 403s — and (b) a stale Secret is a symptom, not a cause. The actual cause is the operator that should have refreshed it. Replacing the Secret with `external-secrets` (an `operator` component) gave the engine a probeable root cause and eliminated the 403.

**Overriding `meta.vars.namespace` per-component.** ESO lives in its own namespace, so the model-wide `default` namespace fell through to the wrong `kubectl -n` argument. The fix isn't to split the model — it's a two-line `vars: { namespace: external-secrets }` on that one component. Any other cross-namespace dependency (ALB Controller, cluster operators) would follow the same pattern.

Each of these refinements was caught by a failing scenario run, not by reading the code. The scenario file is where the engineering judgement lives — the model is the shape, the scenarios are the spec.

---

## Next

- [Model Schema Reference](../reference/model-schema.md) — every field in `system.model.yaml`
- [Scenario Schema Reference](../reference/scenario-schema.md) — every field in scenario files
- [Multi-File Models](../concepts/multi-file-models.md) — splitting the model when the system has distinct operational moments (serving vs. deploy-time)
- [Troubleshooting walkthrough](../concepts/troubleshooting.md) — the runtime loop in more detail
