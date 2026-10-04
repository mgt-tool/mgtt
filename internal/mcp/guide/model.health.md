# `healthy:` — what counts as healthy

Every type has default `healthy` rules (`types_describe` lists them). A component is healthy when every rule holds. It is unhealthy when one is definitively false. It is undecided when a rule reads a fact that could not be read: that component is never cleared, and conclusions name it under "cannot rule out".

## Overriding

A bare list **replaces** the type's rules. Anything you do not restate is gone, and `model_validate` warns about each dropped rule:

```yaml
rds:
  type: rds_instance
  healthy: [connection_count < 500]   # drops available == true: a stopped DB counts as healthy
```

Say what you mean instead:

```yaml
healthy:
  add: [replica_lag < 30]            # the type's rules AND this
```

```yaml
healthy:
  replace: [available == true]       # only this, deliberately (no warning)
```

Use `replace:` for environments where a default is a false positive, for example `cache_hit_ratio > 80` on an idle staging cache, and keep the rule that detects a real outage (`available == true`). If you would restate a type's rules exactly, write no override.

## When rules and states disagree

A type also says what healthy means in its states: the default state is the healthy one. An override that keeps a component healthy in a failure state, or unhealthy in the default one, makes simulate and diagnose disagree on those facts. `model_validate` warns with an example, for instance `healthy rules hold in state saturated (e.g. available=true, connection_count=500)`. Fix the rules, or, when the divergence is deliberate, acknowledge it with `healthy_diverges_from: [saturated]`.

## Thresholds in variables

Some types compare a fact with a per-component threshold: `restart_count <= max_restart_count`. Set it under the component's `vars:` (or `meta.vars:`). `types_describe` lists the variables a provider declares. A threshold set nowhere leaves the rule undecided forever, and `model_validate` warns.

```yaml
web:
  type: container
  vars:
    max_restart_count: 5
```

A bare word in a rule is read by the fact it is compared with. Against a string fact it is a literal (`phase == Bound`). Against any other fact it is another fact of the component, or else a variable.
