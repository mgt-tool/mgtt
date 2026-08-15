# Verification

Verification checks a model over **every configuration it can reach**, rather than over the situations you thought to write down.

It runs at design time, like [simulation](simulation.md): no cluster, no credentials, no probes. The difference is what it takes as input. A scenario gives the engine one set of facts and asks whether it reaches the right conclusion. Verification gives it nothing, and asks what is true of the whole model.

It does not replace `mgtt simulate`. Scenarios are how you say *this specific failure must be diagnosed this specific way* — narrative claims worth reading in a review. Verification covers everything you did not write a scenario for, which is most of it.

---

## What it adds

| | `mgtt simulate` | verification |
|---|---|---|
| Input | Facts you inject | Nothing |
| Covers | The scenarios you wrote | Every reachable configuration |
| A pass means | The engine concluded correctly for these facts | The claim held everywhere, with no exception |
| A failure gives you | The scenario that broke | The shortest sequence of failures that gets there |
| Finds | Wrong conclusions | Configurations nobody considered |

The last row is the one that matters at 3am. A scenario can only be wrong about a case you already imagined. Verification reports the cases you did not.

---

## The pipeline

Three commands, each naming its own step:

```sh
mgtt model export --json  |  mgtt2writ  |  writ check --stdin
```

- **`mgtt model export --json`** emits the resolved model — provider types merged into the components using them, component overrides applied. It is writ-agnostic; a visualiser or an editor can read the same document.
- **[`mgtt2writ`](https://github.com/mgt-tool/mgtt2writ)** translates that into a writ model. It is the only piece that knows both formats.
- **[`writ check`](https://github.com/writ-lang/writ)** enumerates every reachable configuration and answers by exhaustion.

For everyday use, `mgtt-contradict-check` ships with `mgtt2writ` and runs all three, exiting with `writ`'s status:

```sh
mgtt-contradict-check
```

mgtt has no verification subcommand of its own. Nothing in mgtt depends on writ being installed, and nothing in writ knows what mgtt is — the translation belongs to neither and lives on its own. See [ADR 0001](../decisions/0001-where-the-writ-bridge-lives.md).

### Installing the bridge

`mgtt model export` is part of mgtt. The other two are separate installs:

```sh
# the translator, and the wrapper script
git clone https://github.com/mgt-tool/mgtt2writ && cd mgtt2writ && make install

# writ itself
git clone https://github.com/writ-lang/writ && cd writ && make install
```

The intermediate model is an ordinary file you can keep and read:

```sh
mgtt model export --json | mgtt2writ > minishop.writ
```

It is deterministic, so committing it makes architecture changes reviewable as a diff — and gives `writ compare` two revisions to work with. See [Advanced verification](#advanced-verification-with-writ).

---

## The smallest example that pays off

[`examples/minishop`](https://github.com/mgt-tool/mgtt/tree/main/examples/minishop) is two components, three facts, and six reachable configurations — small enough to check by hand, which is the point.

```
api  ──depends on──▶  store
```

`store` is a `datastore` with two facts, `available` and `connection_count`. Its three states are mutually exclusive:

| state | when |
|---|---|
| `live` | `available == true & connection_count < 500` |
| `saturated` | `available == true & connection_count >= 500` |
| `stopped` | `available == false` |

and its health condition names the same two facts, agreeing with `live`:

```yaml
healthy:
  - available == true
  - connection_count < 500
```

Three store states × two api states = six configurations:

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

Exit `0`.

### One reasonable edit

*"A saturated store is still serving — stop paging us for it."* So the health condition is loosened:

```yaml
  store:
    type: datastore
    healthy:
      - available == true
```

`mgtt model validate` stays clean; that is a valid health expression over a valid fact. But `states:` still calls an exhausted pool `saturated`, so the model now calls a component healthy while placing it outside its default active state.

```
equation datastore-store-health-matches-state
  can be broken by: store-fails-saturated, store-fails-stopped   (acknowledge in claims)
  violated in 2 reachable situations   witness: 1. store-fails-saturated
```

Exit `1`. The two are the configurations where `store` is saturated — `available` is true, so the loosened `healthy:` says fine, while `connection_count >= 500` puts it in `saturated` rather than `live`. `api` can be up or down, so that is two. One failure is enough to reach them.

### Reachability is the whole question

An earlier draft of that example put the drift on `connection_count` while no *state* read that fact. The contradiction existed and the checker reported **zero** violations — correctly. Nothing in the model could push `connection_count` over the threshold, so the disagreeing configuration was unreachable.

A contradiction only matters if the system can get into it. A grep over the YAML would find the first kind and be wrong about the second; deciding it is what the exhaustive check is for.

---

## Reading the output

### states and edges

A **state** is one whole-system configuration: what every component is doing at the same moment. An **edge** is one component's failure or one propagation step that leads from one configuration to another.

The names are readable on purpose. `store-fails-saturated` is the store's pool filling on its own. `store-saturated-triggers-api-down` is that reaching the api through the dependency you declared. A sequence of them reads as a failure chain.

### dead ends

A **dead end** is a configuration with nothing left to happen — no further component can fail, and nothing more propagates. Each line is the shortest route to one.

Dead ends are worth reading rather than skipping. They are where the system comes to rest, so they are the configurations your runbook most needs to cover, and they are listed whether or not you expected them.

### equations

An **equation** is a claim that must hold in every configuration. One is written per component type: **a component is healthy exactly when it is in its default active state.**

`can be broken by` lists the moves that *could* invalidate it — a fact about what each move writes, listed whether or not it does so today. It tells you which failures to think about when you next edit that type.

When a claim holds everywhere, that is all you get. There is nothing further to report, and no route to print.

---

## The question a scenario cannot ask

Your model says what "healthy" means for a component. Its type separately says which state it is in. Nothing keeps those two definitions consistent, and when they drift, `simulate` and `diagnose` can consult different facts and disagree — one names a root cause, the other names none.

That drift is not a wrong conclusion about any particular scenario, so no scenario finds it. It is a property of the model. Nothing in `mgtt model validate` looks at it either: its passes cover structure, type resolution, dependency references, cycles, `triggered_by` labels and duplicate resources.

---

## When part of the model does not cross

Some of a model cannot be checked this way, and the translator says so on stderr rather than quietly checking less:

```
declined:
  no predicate mentions this fact, so it has no values worth naming — it is not carried into the model
      first at: datastore.connection_count
```

Read these before you read the report. They tell you which parts of your model the run did not cover, so a clean result is never mistaken for a claim about the whole thing. Some are worth investigating on their own: a state nothing can satisfy is a state your system can never be diagnosed as being in.

A decline is not a finding by default — a large model usually has some. Pass `--strict` to `mgtt2writ` to make one cost exit `1`, which is the shape a CI check wants once you have read them all once.

---

## In CI

Verification needs nothing that simulation does not, so it goes in the same job:

```yaml
      - name: validate model
        run: mgtt model validate

      - name: run scenarios
        run: mgtt simulate --all

      - name: check the model for contradictions
        run: mgtt-contradict-check
```

Scenarios block the PR when someone breaks a case you wrote down. Verification blocks it when someone breaks one you did not.

---

## Advanced verification with writ

`writ check` answers one question. The translated model is an ordinary writ model, so every other writ verb applies to it too — and the expensive part, the translation, is already done.

All of the following run against `examples/minishop`.

### `writ show` — what a situation actually is

`check` reports a violation as a count and a witness route. `show` tells you what you arrive at:

```sh
writ show minishop-drifted.writ --at 2
```

```
situation 2 of 6
  cells:   (store.available=yes store.connection-count=at-or-above-500 api.reachable=yes)
  route:   1. store-fails-saturated
  moves:   api-fails-down → 4   store-saturated-triggers-api-down → 4
```

Every fact, the shortest route in, and what can happen next. This is the natural second command after a finding.

### `writ compare` — did this PR change the failure space?

Translate the model before a change and after, then diff the *laws*, not the YAML:

```sh
writ compare minishop.writ minishop-drifted.writ
```

```
equations:   datastore-health-matches-state        LOST
             service-health-matches-state          preserved
             datastore-store-health-matches-state  gained
properties:
```

The loosened health condition lost the type-level law and gained a weaker per-component one. Exit `1`, so this is a CI check on its own — architecture regression detection that no mgtt command can perform. Commit the generated `.writ` and `writ compare --git HEAD~1 HEAD minishop.writ` does it across revisions directly.

### `writ derive` — name the situations, don't just count them

A `.rules` file names a set the interrogator already computed. `examples/minishop/minishop.rules`:

```lisp
(relation saturated 1)
(rule (saturated S)
  (situation S)
  (holds S (and (is store.available yes)
                (is store.connection-count at-or-above-500))))
```

```sh
writ derive minishop-drifted.writ minishop.rules saturated
```

```
saturated  (2 rows)
  2
  4
```

Situations 2 and 4 — exactly the two `check` counted. Where `check` gives a number and one witness, `derive` hands you the whole set to work through. Recursive relations work too; `can-lose-api` in the same file walks backward over the edges from a goal.

`holds` does not bind its situation, so a rule must range over `situation` first — a body joins in written order.

### `writ query` — bind entities at a situation

A `.claims` file sits beside the model, named after it. `examples/minishop/minishop.claims`:

```lisp
(query saturated-stores
  (where (s datastore))
  (and (is s.available yes)
       (is s.connection-count at-or-above-500)))
```

```sh
writ query minishop.writ saturated-stores --at 2
```

```
saturated-stores  (at state 2)
  s = store
```

With one store this is a small answer. On a real model it is *"which of my forty components are in this condition, right here"* — and the same query can be asked of every situation the checker found.

### `.claims` acknowledgements — drift detection for your edit history

`check` lists, for each law, the moves that write a fact the law reads. Accepting one records that you know about it:

```lisp
(accept store-fails-saturated datastore-health-matches-state)
(accept store-fails-stopped   datastore-health-matches-state)
```

```sh
writ check minishop.writ --claims minishop.claims
```

Two findings fall out, and both cost exit `1`. A move that can break a law and was never accepted is **undeclared** — usually a move you just added, or a law you just widened:

```
unadmitted  store-fails-saturated may break datastore-health-matches-state
```

A move accepted against a law it cannot touch is **stale** — usually a law that was narrowed and an acknowledgement nobody removed. Keeping the acknowledgements in the repository is what turns the next model edit into a reviewable event.

### `writ control` and `writ schema` — the model as data

```sh
writ control minishop.writ    # the move vocabulary, as a `quiver` instance
writ schema  minishop.writ    # the types, arrows and laws, as an `olog` instance
```

```
(instance minishop-control quiver
  (node n0)
  (edge api-fails-down (src n0) (tgt n0))
  (edge store-fails-saturated (src n0) (tgt n0))
  ...
```

These emit the *vocabulary* — the move names and the schema shape — not the reachability graph, which is what `check` and `derive` report on. Useful when another tool needs to consume the model's structure; niche otherwise.

---

## Reference

- [Simulation](simulation.md) — scenarios, and the failures they are for
- [Troubleshooting](troubleshooting.md) — the same model at runtime
- [Model Schema Reference](../reference/model-schema.md) — `healthy`, `states`, and `default_active_state`
- [ADR 0001](../decisions/0001-where-the-writ-bridge-lives.md) — why the bridge is a separate tool
- [`examples/minishop`](https://github.com/mgt-tool/mgtt/tree/main/examples/minishop) — the worked example on this page
