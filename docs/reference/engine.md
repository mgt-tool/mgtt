# Engine Reference

How mgtt's constraint engine picks probes, narrows the search, and terminates — with the complexity math for sizing a model.

This page covers the internals. For a high-level tour, read [How It Works](../concepts/how-it-works.md) first.

## On this page

- [The loop](#the-loop) — what runs for every probe
- [Strategies](#strategies) — BFS, Occam, AutoSelect, standalone-unhealthy
- [Decision outcomes](#decision-outcomes) — Probe, Done, Stuck
- [Probe selection heuristics](#probe-selection-heuristics) — symptom-inward walk, cross-elimination, suspect hints
- [Scenario enumeration](#scenario-enumeration) — when to pre-compute the chain table
- [Complexity](#complexity) — how many probes and scenarios to expect
- [Worked example](#worked-example) — applied to a 22-component model

---

## The loop

`mgtt diagnose` is a fixed-point loop: ask the strategy for the next probe, run it, append the result to the fact store, repeat. Termination is driven by the strategy's `Decision`, not by the loop itself.

```
for probesRun < maxProbes:
    if ctx.deadline_exceeded: report_partial(); stop
    decision = strategy.SuggestProbe(model, registry, store, scenarios, suspects)
    switch decision:
        Done  → report_done(decision.root_cause); stop
        Stuck → report_stuck(); stop
        Probe → run(decision.probe); append fact to store
```

The loop is stateless between iterations — all state lives in the fact store. This means a run can be paused, serialised (`mgtt incident end`), and resumed hours later without replaying anything.

## Strategies

mgtt ships two first-class strategies plus a post-termination safety net. Pick one manually, or let `AutoSelect` choose.

### AutoSelect

Picks `Occam` when the input carries any enumerated scenarios; `BFS` otherwise. The decision is re-made every iteration, which matters once — it doesn't matter to the operator, but it lets future strategies opt in mid-run.

```go
if len(in.Scenarios) > 0 { return Occam() }
return BFS()
```

### Occam

Scenario-driven. Treats each pre-enumerated failure chain as a hypothesis and narrows the live set by asking "which probe would eliminate the most chains at once?"

```
live = FilterLive(scenarios, store)
if |live| == 0: return Stuck("no scenario matches observed facts")
if |live| == 1: return Done(live[0] as root cause)

sort live by:
  1. chain length (shortest first — fewer moving parts wins ties)
  2. touches-any-suspect? (operator-supplied hints come first)
  3. cross-elimination count (probes that invalidate the most other chains)
  4. chain ID (deterministic tiebreak)

chosen = live[0]
return Probe(pickSymptomInward(chosen))
```

Live-set filtering is the heart. A chain step is *contradicted* iff its state's `when` predicate evaluates to `false` under collected facts; *confirmed* iff it evaluates to `true`; *undefined* (and the chain stays live) iff any fact it depends on is missing. An evaluator error that isn't `UnresolvedError` treats the step as contradicted — that way a bad predicate can't keep every chain alive forever.

### BFS

Graph-traversal. No hypothesis table; just walks the dependency graph from the entry point (the component nothing else depends on) outward, probing every reachable fact in deterministic order.

```
visited = {}
queue   = [entry]
while queue:
    c = queue.pop_front()
    visited.add(c)
    for dep in c.depends:
        queue.push_back(dep)

for c in visited (BFS order):
    for fact_name in sorted(type.facts):
        if not store.has(c, fact_name):
            return Probe(c, fact_name)

return Done(no root cause)  # post-check runs now, see below
```

BFS is the fallback when no scenarios are enumerated — which is the common case for large models (see [`meta.scenarios: none`](model-schema.md)). It's correct but unscoped: it will happily probe every leaf even when the first-layer diagnosis is obvious. Real models rely on `--max-probes` and `--deadline` to bound it.

### Standalone-unhealthy check

After BFS exhausts reachable facts, the engine scans every component's `healthy:` predicate against the collected facts. If any rule evaluates definitively false, the most-upstream offender is returned as root cause — no chain, just a one-step synthetic scenario (`external-secrets.not_running`).

Without this step, a cluster with a broken upstream and a still-serving user layer reports `Root cause: (none — all components healthy)` — correct by algorithm (no downstream symptom → no chain to walk), wrong by UX. The check closes that gap.

Upstream selection: among unhealthy components, those whose direct deps are all healthy (or unresolved) win. Component-level `healthy:` overrides take precedence over type-level rules, so a model-specific threshold (`connection_count < 500`) overrides the provider default.

### Summary

| Strategy | Input needed | Output | When it runs |
|---|---|---|---|
| **Occam** | enumerated scenarios | root cause from `live[0]` when `|live| == 1` | `scenarios: <path>` or `scenarios: auto` in model |
| **BFS** | none | `Done` or `Probe`; no root cause from chain walk alone | `scenarios: none` or scenarios list empty |
| **Standalone check** | fact store + component healthy rules | `Done(root)` when upstream unhealth with no explaining chain | after BFS coverage exhausted |

## Recoverable probe errors

Not every failed probe is fatal. The engine distinguishes error *classes* so that a single RBAC hole or transient throttle doesn't force the operator to re-run the whole session.

| Error class | Meaning | Engine behaviour |
|---|---|---|
| `provider: not_found` | Probe ran; the target resource does not exist. | Fact stored with `Status=not_found`. Any scenario requiring a non-default state for that component is contradicted. Engine continues. |
| `provider: forbidden` | Probe ran; IAM / RBAC refused read. | Fact stored with `Status=forbidden`, `Value=nil`. Expression layer treats as `UnresolvedError` (unknown, not false). Engine continues. Report adds a "Partial visibility" line counting these. |
| `provider: transient` | Throttled / timed out / temporary upstream outage. | Same as forbidden — unknown, report flags. |
| `provider: usage` / `env` / `protocol` / `unknown` | Bug or misconfiguration. | Hard fail. Diagnose loop aborts with an error. |

The "unknown, not false" distinction matters: a forbidden probe must never silently flip a component's state to its failed branch, or the engine would rewrite a chain based on an RBAC hole. Instead the fact stays unresolved and the engine either picks a different probe, or — if nothing else distinguishes the live set — terminates with the partial-visibility warning prominently displayed.

```
Root cause: rds
Scenario:   rds.stopped → app.crash_looping
Probes run: 42/100   Time: 1m10s/3m0s
Partial visibility: 2 forbidden (RBAC / IAM refused) — result may be incomplete.
Trail: …
```

## Decision outcomes

Every `SuggestProbe()` returns one of:

```go
type Decision struct {
    Probe     *Probe               // suggest running this probe next
    Done      bool                 // terminate the loop
    RootCause *scenarios.Scenario  // non-nil → name this as root cause
    Stuck     bool                 // observed facts contradict every chain
    Reason    string               // human-readable tag
}
```

- `Probe != nil` — run it, loop.
- `Done == true && RootCause != nil` — print the chain.
- `Done == true && RootCause == nil` — print "all components healthy".
- `Stuck == true` — print the stuck report (model-gap territory).

The loop also exits if the probe budget runs out (`--max-probes`) or the wall-clock deadline passes (`--deadline`). These are bounds on the loop, not on the strategy.

## Probe selection heuristics

### Symptom-inward walk

Occam, once it picks a chain, walks it **terminal → root** and returns a probe for the first step not yet fact-level verified. A step is verified iff every fact it directly relies on is already in the store — `Observes` for terminal steps, or every fact named in the state's `when` predicate for non-terminal steps. The old component-level check (any fact → skip) was too coarse: it skipped a step even when only one of its four state-facts was known.

This matters because symptoms are cheap and unambiguous — checking `web.ready_replicas` once tells you whether the whole chain's terminal assertion holds. You don't want to probe three upstream layers only to discover the symptom never materialised.

### Cross-elimination ranking

When multiple live chains share a component, a probe on that component invalidates every chain whose state predicate the probe contradicts — not just the chain that suggested it. Occam scores each candidate chain by how many *other* chains its symptom-inward probe would eliminate; ties in length and suspect-overlap break on this score (higher first).

### Suspect hints

`mgtt diagnose --suspect <comp>` or `--suspect <comp>.<state>` gives Occam a soft prior, not a filter. Chains that touch the named component (in the named state, if specified) are sorted before chains that don't, after the length sort. The engine still considers every live chain; suspect just reorders ties.

The report tells you how the hint landed:

- `confirmed as root` — suspect is at the final chain's root;
- `appeared mid-chain` — suspect was downstream of the real cause;
- `ignored` — the final chain doesn't touch the suspect at all.

## Scenario enumeration

Scenarios are the chain table Occam consumes. They're enumerated offline (once per model change) by walking the causation DAG declared in providers' `failure_modes`/`triggered_by`, pruned by model-declared dependencies.

A *scenario* is a path `root_state → … → terminal_state` where each arrow is a causation edge and the terminal component has an observable fact (`Observes`). Enumeration stops at observables because Occam's job is to pick a probe — there's nothing to probe past the leaf.

Control enumeration via the model's `meta.scenarios` field:

| Value | Effect |
|---|---|
| `auto` (default) | regenerate on every `mgtt diagnose` / `mgtt simulate --from-scenarios` |
| `<path>.yaml` | pre-computed sidecar; skip regeneration, load from disk |
| `none` | don't enumerate; BFS will run instead |

Size is no longer a reason for `none`. The sidecar stores the failure graph (11 KB for a 12,755-chain model), and Occam prices every candidate in one pass (about 37 ms per decision at that size). Use `none` for placeholder models that do not yet describe failure modes.

## Complexity

Let:

- `N` = components in the model
- `F` = average facts per component type (typically 3–8)
- `B` = average fan-out of `can_cause` per failure state (2–5 in real providers)
- `L` = average chain length from root to terminal (depends on graph depth)
- `R` = number of *root states* — failure states on components that nothing depends on

### BFS complexity

```
probes         ≤ N × F          (every reachable fact, worst case)
probes typical ≈ 0.4 × N × F    (most probes resolve a branch in 1-2 facts)
memory         = O(N + total_facts_collected)
```

BFS has no upper-bound conceptually other than the graph; the `--max-probes` flag is the operational bound.

### Scenario enumeration complexity

For a causation DAG with branching factor `B` and path length `L`:

```
total scenarios    ≈ R × B^L
scenario storage   = the failure graph: O(states + edges), not O(total)
Occam live_set max = |total| initially, shrinks monotonically
```

The `B^L` term is why scenario counts explode for deeply-connected graphs. Storage no longer pays for it, since `scenarios.yaml` holds the graph. Diagnosis still does, because it expands every chain into memory when it loads. Measured on layered synthetic models (width 10, fan-out 2, 4 cores; `BenchmarkScale` in `internal/engine/strategy`):

| Tiers | Components | Chains | Expand on load | One Occam decision |
|---|---|---|---|---|
| 4 | 41 | 10,921 | 50 ms | 15 ms |
| 5 | 51 | 66,181 | 0.4 s | 128 ms |
| 6 | 61 | 397,921 | 2.7 s | 0.7 s |
| 7 | 71 | 2,388,541 | 20 s | 5.5 s |
| 8 | 81 | did not finish in 60 s | | |

**Depth, not component count, sets the cost.** A 300-component model six tiers deep expands to about two million chains. The 20-component storefront example, seven tiers deep but narrow, expands to 12,755. Past six or seven tiers of real fan-out, use `meta.scenarios: none` (BFS) until diagnosis counts paths through the graph instead of listing them (planned).

### Occam complexity

Per iteration:

```
filter_live:         O(|live| × L)            evaluate every step
sort:                O(|live| × log |live|)   (elimination counts priced once per round)
pick_symptom_inward: O(L)
```

Practical cost per round is dominated by `filter_live` and the one-pass ranking. For a 10,000-chain model one round is about 15 ms, and probe latency (seconds, sometimes) dwarfs it. The constraint is how many chains there are to hold and filter, which the table above shows growing with depth.

## Worked example

A mid-sized reference model — call it **blue-green HTTP service** — 22 components, covering an edge/CDN layer, an ALB ingress, service + deployment tiers for two colors (blue and green), a managed data layer (relational DB, cache, message queue, object store), a search node, a config operator, and two business-process components on top of cron and async workers:

```
N = 22 components
F ≈ 5 facts/component (operator has 4, deployment has 8, db has 2, …)
N × F = 110     theoretical max probes under pure BFS
```

A run against a clean cluster:

```
Probes run: 66/100   Time: 1m10s/3m0s
Root cause: (none — all components healthy)
```

66 probes, well under the 110 ceiling. The gap is because some facts are never reached — BFS visits every component, but generic components with `operator_says_healthy` pre-seeded skip the provider path entirely.

**Scenario count if enumerated.** The model carried `meta.scenarios: none`, from before the sidecar stored a graph and before Occam priced candidates in one pass. Neither cost applies any more. The note in its header read:

> scenarios.yaml is huge. The model enumerates ~50k chains (≈40MB YAML).

That 50 000 chains arises from the blue/green doubling and the 6-deep dep graph: `edge → ingress → svc → web-{blue,green} → app-{blue,green} → db|cache|queue|search|bucket`. Each failure mode at the data layer (`db.stopped`, `cache.unreachable`, `queue.degraded`, …) propagates through two colors × two tiers × a `business_process` symptom layer, and provider-level `can_cause` branches multiply further.

Written out, `B ≈ 3`, `L ≈ 8`, `R ≈ 8` root states → `R × B^L ≈ 8 × 3^8 ≈ 52 000`. Matches the observed count.

With scenarios disabled, Occam never runs for this model. BFS probes the graph, the standalone-unhealthy check scans the healthy predicates, and the report looks like:

```
Root cause: config-operator
Scenario:   config-operator.not_running
Probes run: 66/100   Time: 1m10s/3m0s
Trail:
  …
  41. config-operator.crd_registered      = true
  42. config-operator.deployment_ready    = false   ← rule broken
  43. config-operator.restart_count       = 0
  …
```

— exactly the diagnosis Occam would have produced from an enumerated `config-operator-down.yaml` scenario, without the 40 MB sidecar.

## See also

- [How It Works](../concepts/how-it-works.md) — conceptual tour
- [Model Schema](model-schema.md) — `meta.scenarios`, `healthy:`, `depends:`
- [Scenario Schema](scenario-schema.md) — hand-authored test scenarios
- [`scenarios.yaml`](scenarios-yaml.md) — the enumerated sidecar
