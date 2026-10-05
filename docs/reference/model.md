# Model and scenario reference

## `system.model.yaml`

```yaml
meta:
  name: storefront                 # required
  version: "1.0"                   # required, quoted
  providers: [mgt-tool/kubernetes@>=3.0.0]
  vars: { namespace: production }  # substituted as {namespace} in probes
  strict_types: false              # true: an untyped component is an error
  scenarios: none                  # opt out of scenarios.yaml and its drift check

components:
  api:
    type: kubernetes.deployment    # required: <provider>.<type>, or bare <type>
    resource: shop-api-{env}       # real resource name; {var} placeholders allowed
    source: discovered             # written by `model build`; absent = authored, never deleted by build
    providers: [mgt-tool/aws]      # override meta.providers for this component
    vars: { namespace: payments }  # override meta.vars
    depends:
      - on: rds
      - on: vault
        while: vault.state == "starting"   # edge exists only while true
      - on: [web-a, web-b]
        need: 1                    # redundancy group: holds while ≥1 is healthy
    healthy:
      add: [restart_count < 3]     # the type's rules AND these
    failure_modes:                 # extra propagation the type doesn't declare
      degraded: { can_cause: [upstream_5xx] }
```

`source:` says where a component came from. `mgtt model build` writes `source: discovered` on what it found. Any other component is authored: business processes, external services, hand-written wiring. A rebuild keeps authored components whether discovery returns them or not, and names them as kept. Only a discovered component that discovery stops returning goes through the deletion gate (`--allow-deletes`, `--tombstone`).

### Health rules

`healthy:` has three forms:

- `add: […]` keeps the type's default rules and also requires yours.
- `replace: […]` uses only your rules.
- A bare list also replaces the type's rules, and `validate` warns about each type rule it drops. A redis rule of `cache_hit_ratio > 70` alone would no longer check `available == true`.

Rules have the form `<fact> <op> <value>`, with `==`, `!=`, `<`, `>`, `<=` or `>=`. All rules must hold. Values can be numbers, booleans or quoted strings. A bare word is read as follows:

- against a string fact, it's a literal (`phase == Bound`);
- otherwise, if it's a fact of the same component, it means that fact (`ready_replicas == desired_replicas`);
- otherwise it's a variable, looked up in the component's `vars`, then `meta.vars`, then the provider's default.

A variable that's set nowhere leaves the rule undecided, and `validate` warns.

A type says what healthy means twice: in its rules, and in which of its states is the default. If an override makes them disagree, `validate` warns and gives the facts where it happens. Rules that are loosened so they still hold in `saturated` mean simulate (which reads states) and diagnose (which reads rules) disagree there. When the divergence is deliberate, list those states:

```yaml
opensearch:
  type: deployment
  healthy:
    replace: [ready_replicas >= 1]
  healthy_diverges_from: [crashed, degraded]   # one ready replica serves search on stage
```

Disagreements in a type's own defaults are the provider's to fix; `mgtt provider validate` reports them.

### Dependencies

`api` depends on `rds` means a broken `rds` can break `api`. Use `while:` for a conditional edge, such as a blue/green service that follows its live color (`while: selector_value == blue`). A redundancy group with `need:` holds while at least that many members are *proven* healthy. A member that couldn't be read doesn't count. Active/passive pairs are not groups: model them with `while:`.

### Several models

One repo can hold several models, for example `edge.model.yaml` and `data.model.yaml`. Each command takes `--model <path>`. Models do not import one another.

## Scenarios: `scenarios/*.yaml`

```yaml
name: rds forbidden
description: IAM denies every rds probe; api is crashing.
entry: nginx                       # where diagnosis starts; default: the model's entry point
inject:                            # component → fact → value
  api: { ready_replicas: 0, desired_replicas: 3, restart_count: 9 }
unresolved:                        # probes that ran but produced no value
  rds: { available: forbidden, connection_count: forbidden }
expect:
  root_cause: api                  # required; `none` for all-healthy
  path: [nginx, api]               # ordered subsequence of the actual path
  eliminated: [frontend]           # subset of the actual list
  cannot_rule_out: [rds]           # left undecided by forbidden or transient facts
  not_eliminated: [rds]            # must stay in play
  redundancy_degraded: [web-a]     # broken members their group absorbed
```

- **`unresolved`** outcomes are `forbidden`, `transient` (both unknown, so the component is kept in play) or `not_found`. If every fact of a component is `not_found`, the component is absent, and it can be the root cause.
- **Inject enough facts** for the state you mean. `ready_replicas: 0` without `restart_count` can resolve to `degraded` rather than `crashed`.
- A component you don't mention has no facts, so it is never eliminated.
- **`entry`** names the component diagnosis starts from, as `plan --component` does. Set it for a failure the model's entry point can't reach, such as a background job no request path touches.

Run them with `mgtt simulate --all`, or `--scenario <file>` for one. `--from-scenarios` checks every enumerated chain. `--fuzz N` checks that the engine reaches a conclusion from random, partial evidence.

## `scenarios.yaml` (generated)

`mgtt model validate --write-scenarios` writes every failure chain the model allows, stored as a failure graph (`format: graph/v1`). Don't edit it. Regenerate it and commit it. `validate` fails once the model would produce a different graph; comments and version bumps don't count. `validate --check-scenarios` runs only that check.
