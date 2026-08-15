# Multi-File Models

One system, several model files. Each file is a contract for one *moment*.

## On this page

- [Show me](#show-me) — three tiny examples, side by side
- [Why split at all?](#why-split-at-all)
- [When to split](#when-to-split) — and when NOT to
- [Naming pattern](#naming-pattern)
- [File header convention](#file-header-convention)
- [Switching between models](#switching-between-models)

---

## Show me

Three model files for the same Magento storefront — each is a contract for one operational moment. None of them tries to do the others' job.

### 1. Steady state — always loaded

`magento.model.yaml`

```yaml
checkout_slo:
  type: tracing.span_invariant
  vars:
    span: "checkout.init"
    target_max: 800ms
    target_max_error_rate: 0.001        # 0.1% errors
    breach_tolerance_seconds: 30        # 30s sustained = page someone

cart_slo:
  type: tracing.span_invariant
  vars:
    span: "cart.add"
    target_max: 500ms
    target_max_error_rate: 0.005
    breach_tolerance_seconds: 60
```

### 2. The deploy moment — loaded right after a blue/green switch

`magento-canary.model.yaml`

```yaml
post_switch_canary:
  type: tracing.span_invariant
  vars:
    span: "http.server.request"
    span_filter: 'resource.deployment.color = "green"'   # only the new color
    target_max: 3s                                       # cold-cache headroom
    target_max_error_rate: 0.005                         # tighter than steady-state
    breach_tolerance_seconds: 0                          # zero tolerance now
```

### 3. The migration window — loaded only while a schema change is in flight

`magento-migration.model.yaml`

```yaml
row_count_drift:
  type: aws.rds_query
  vars:
    query: "SELECT COUNT(*) FROM orders"
    target_max_drift: 0.001              # < 0.1% row delta vs baseline

terraform_drift:
  type: terraform.resource
  vars:
    address: aws_db_instance.main        # config-vs-reality during the change
```

Three files, one storefront. Operators load whichever matches the moment they're in:

```bash
export MGTT_MODEL=magento.model.yaml           && mgtt plan   # 99% of the time
export MGTT_MODEL=magento-canary.model.yaml    && mgtt plan   # post-deploy gate
export MGTT_MODEL=magento-migration.model.yaml && mgtt plan   # during DB change
```

That's the whole pattern. Below is the rationale and the conventions.

---

## Why split at all?

A model is a *contract* mgtt evaluates against the live system. Different operational moments call for different contracts:

- The steady-state error budget is too loose for a fresh canary fleet.
- The migration window's row-count invariant is meaningless after the migration completes.
- Stuffing all three into one file means every probe runs all the time, and a reader can't tell which numbers are "in force" right now.

Separate files = each moment gets a contract that fits it. Operators load one at a time.

## When to split

Split when the new file answers a **different question**. Three reliable signals:

| Signal | One file | Separate files |
|---|---|---|
| **Operational moment** | "all the time" | a specific window: deploy, migration, incident |
| **Components in scope** | the same set | a different slice (one team, one tier, one region) |
| **Numeric SLOs** | one contract per component | tighter or looser bounds *for the same components* during an event |

### When NOT to split

- **Different environments** (staging vs prod) — same shape, different numbers. Use vars or per-env override files, not duplicated component definitions.
- **One-var differences** — if file B is "file A but with `target_max: 2s` instead of `1s`", it's a var override.
- **Hypothetical future scenarios** — add a model when there's an actual moment to invoke it for, not before.

## Naming pattern

Filenames read as noun phrases:

```
<system>.model.yaml                # the canonical baseline
<system>-<moment>.model.yaml       # a specific operational window
<system>-<scope>.model.yaml        # a narrowed view (one team, tier, region)
```

The baseline owns the unmodified system name; specialized files hyphenate.

## File header convention

Every model file should open with three things, in this order:

1. **What question this file answers** — one sentence in plain operator-language.
2. **When to invoke it** — what operational moment makes this model the right contract.
3. **Companion models** — one-line pointer to siblings in the same directory, so a reader can navigate the family without consulting external docs.

```yaml
# Magento storefront — observed during the moment of a blue/green deploy,
# when steady-state SLOs aren't the right contract.
#
# Invoke right after the selector flip, as the "are we OK to keep going?"
# gate before scaling the old color down.
#
# Companion models:
#   • magento.model.yaml — steady-state SLOs (always loaded)
```

## Switching between models

`mgtt plan` and `mgtt simulate` read one model at a time, selected via `MGTT_MODEL`. In CI / deploy pipelines, the operational moment selects which model is in scope:

```bash
# Post-deploy hook — wait for spans to land, then run the canary contract
sleep 60
MGTT_MODEL=magento-canary.model.yaml mgtt plan --fail-on-breach
```

Models don't compose at load time today — each file is read in isolation. Cross-file references *are* deliberate: a canary model's `depends_on` can name a `kubernetes.service` component without redefining the entire infra layer, because that component already exists in the running cluster — the model is a *view* over reality, not a definition of it.
