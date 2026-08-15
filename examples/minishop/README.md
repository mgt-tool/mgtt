# minishop — the smallest model that shows verification paying off

Two components, three facts, six reachable configurations. Small enough to
check by hand, which is the point: you can confirm the tool is right rather
than take its word for it.

Everything here is fictional and self-contained. The provider ships in
`mgtt-home/`, so nothing is installed and no credentials are read.
Verification is a design-time operation over the model alone — the probe
commands in the provider are illustrative and are never run.

## Setup

```sh
cd examples/minishop
export MGTT_HOME=$PWD/mgtt-home
```

## The system

```
api  ──depends on──▶  store
```

`store` is a `datastore`, with two facts: `available` and `connection_count`.
`api` is a `service` with one fact, `reachable`, and its `down` state is
`triggered_by: [connection_refused]` — which is what lets a store failure
propagate across the dependency edge.

The `datastore` type has three states, and they are mutually exclusive:

| state | when |
|---|---|
| `live` | `available == true & connection_count < 500` |
| `saturated` | `available == true & connection_count >= 500` |
| `stopped` | `available == false` |

Its health condition names the same two facts, and agrees with `live`:

```yaml
healthy:
  - available == true
  - connection_count < 500
```

Three store states × two api states = **six configurations**, which is exactly
what the checker reports.

## The clean run

```sh
mgtt model export --json model.yaml | mgtt2writ | writ check --stdin
```

```
states: 6   edges: 9
gaps: none
dead ends: 2
  reached by: api-fails-down, store-fails-saturated
  reached by: api-fails-down, store-fails-stopped
equation datastore-health-matches-state
  can be broken by: store-fails-saturated, store-fails-stopped   (acknowledge in claims)
equation service-health-matches-state
  can be broken by: api-fails-down, store-saturated-triggers-api-down, store-stopped-triggers-api-down   (acknowledge in claims)
```

Exit `0`. Health and state agree in all six configurations.

## The breaking change

`model-drifted.yaml` makes one edit, and it is a reasonable-sounding one — *"a
saturated store is still serving, stop paging us for it"*:

```yaml
  store:
    type: datastore
    healthy:
      - available == true
```

`mgtt model validate` stays clean: that is a valid health expression over a
valid fact. But the type's `states:` block still calls an exhausted pool
`saturated`, so the model now calls a component healthy while placing it
outside its default active state.

```sh
mgtt model export --json model-drifted.yaml | mgtt2writ | writ check --stdin
```

```
equation datastore-store-health-matches-state
  can be broken by: store-fails-saturated, store-fails-stopped   (acknowledge in claims)
  violated in 2 reachable situations   witness: 1. store-fails-saturated
```

Exit `1`. Two of the six configurations disagree with themselves, and one
failure is enough to reach them.

**Check it by hand.** The two are the ones where `store` is saturated: `store`
is `available`, so the loosened `healthy:` says fine, while
`connection_count >= 500` puts it in `saturated` rather than `live`. `api` can
be either up or down, so that is two configurations. The witness is the single
move `store-fails-saturated`.

## Why no scenario finds this

`mgtt simulate` injects facts and checks the conclusion, so it can only be
wrong about a case someone already imagined. This is not a wrong conclusion
about any particular case — it is a property of the model, true regardless of
which facts you feed it. Nothing in `mgtt model validate` looks at whether
`healthy:` and `states:` agree.

Left in place, it makes `simulate` and `diagnose` read different facts and
disagree: one names a root cause, the other names none.

## Reachability is the whole question

An earlier draft of this example put the drift on `connection_count` while no
state read that fact. The contradiction existed and the checker reported
**zero** violations — correctly, because nothing in the model could push
`connection_count` over the threshold, so the disagreeing configuration was
unreachable.

That is the distinction worth taking away. A contradiction only matters if the
system can get into it, and computing that is what the exhaustive check is
for. A grep over the YAML could find the first kind and would be wrong about
the second.

## Going further

`minishop.claims` and `minishop.rules` carry this example into the rest of
writ's verbs — naming the violating situations rather than counting them,
diffing two versions of the architecture, and acknowledging which moves are
allowed to touch which law.

See [Advanced verification](../../docs/concepts/verification.md#advanced-verification-with-writ).
