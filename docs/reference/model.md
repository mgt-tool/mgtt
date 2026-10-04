# Model and scenario reference

## `system.model.yaml`

```yaml
meta:
  name: storefront                 # required
  version: "1.0"                   # required, quoted
  providers: [mgt-tool/kubernetes@>=3.0.0]
  vars: { namespace: production }  # substituted as {namespace} in probes
  strict_types: false              # true: an untyped component is an error
  scenarios: none                  # skip scenarios.yaml (very large models)

components:
  api:
    type: kubernetes.deployment    # required: <provider>.<type>, or bare <type>
    resource: shop-api-{env}       # real resource name; {var} placeholders allowed
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

### Dependencies

`api` depends on `rds` means a broken `rds` can break `api`. Use `while:` for a conditional edge, such as a blue/green service that follows its live color (`while: selector_value == blue`). A redundancy group with `need:` holds while at least that many members are *proven* healthy. A member that couldn't be read doesn't count. Active/passive pairs are not groups: model them with `while:`.

### Several models

One repo can hold several models, for example `edge.model.yaml` and `data.model.yaml`. Each command takes `--model <path>`. Models do not import one another.

## Scenarios: `scenarios/*.yaml`

```yaml
name: rds forbidden
description: IAM denies every rds probe; api is crashing.
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

Run them with `mgtt simulate --all`, or `--scenario <file>` for one. `--from-scenarios` checks every enumerated chain. `--fuzz N` checks that the engine reaches a conclusion from random, partial evidence.

## `scenarios.yaml` (generated)

`mgtt model validate --write-scenarios` writes every failure chain the model allows. Don't edit it. Regenerate it and commit it. `validate` fails when its `source_hash` no longer matches the model and types. `validate --check-scenarios` runs only that check.
