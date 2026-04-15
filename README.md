![mgtt](docs/images/mgtt_full_lockup.png)

## model guided troubleshooting tool

If you build or maintain anything with more than two components, you know the drill: something stops working, you open three terminals and start guessing.

**mgtt fixes this.** You describe your system's dependencies once in a YAML model. When something breaks, a constraint engine walks the dependency graph, probes components in order of information value, and eliminates healthy branches. It always knows what to check next and why.

And before the system even exists, you can simulate failures against the model to verify the reasoning is correct — like unit tests for your architecture.

## See it in action

### Simulation: catch model gaps in CI

```
$ mgtt simulate --all

  rds unavailable                          ✓ passed
  api crash-loop independent of rds        ✓ passed
  frontend crash-looping, api healthy      ✓ passed
  all components healthy                   ✓ passed

  4/4 scenarios passed
```

No running system. No credentials. Runs on every PR.

### Troubleshooting: root cause in 6 probes

Monday 3am. Alert fires. You run `mgtt plan` and press Y:

```
$ mgtt plan

  -> probe nginx upstream_count         ✓ nginx.upstream_count = 0   ✗ unhealthy
  -> probe api restart_count            ✓ api.restart_count = 47     ✗ unhealthy
  -> probe rds available                ✓ rds.available = true       ✓ healthy  ← eliminated
  -> probe frontend ready_replicas      ✓ frontend.ready_replicas = 2  ✓ healthy  ← eliminated

  Root cause: api
  Path:       nginx <- api
  Eliminated: frontend, rds
```

4 components probed, 2 eliminated, root cause found. You didn't need to know the system — the model knew it for you.

---

## Install

```bash
curl -sSL https://raw.githubusercontent.com/mgt-tool/mgtt/main/install.sh | sh
```

Or: `go install github.com/mgt-tool/mgtt/cmd/mgtt@latest` | Or: `docker run --rm -v $(pwd):/workspace ghcr.io/mgt-tool/mgtt`

## Quick start

```bash
mgtt init                          # scaffold system.model.yaml
mgtt model validate                # check the model
mgtt provider install kubernetes   # install providers
mgtt simulate --all                # run failure scenarios
mgtt plan                          # troubleshoot a live system
```

**Two modes, same model:**

| | Design time | At 3am |
|---|---|---|
| Command | `mgtt simulate` | `mgtt plan` |
| Facts from | Scenario YAML | Real probes (kubectl, aws) |
| Output | Pass/fail assertions | Guided root cause |

---

## Documentation

- [Quick Start](./docs/getting-started/quickstart.md) — complete end-to-end example
- [How It Works](./docs/concepts/how-it-works.md) — the constraint engine and dependency graph
- [Simulation](./docs/concepts/simulation.md) — design-time model validation
- [Troubleshooting](./docs/concepts/troubleshooting.md) — runtime incident response
- [Model Schema](./docs/reference/model-schema.md) — every field in `system.model.yaml`
- [Scenario Schema](./docs/reference/scenario-schema.md) — every field in scenario files
- [Type Catalog](./docs/reference/type-catalog.md) — all provider types, facts, and states
- [CLI Reference](./docs/reference/cli.md) — every command
- [Provider Registry](./docs/reference/registry.md) — official and community providers
- [Writing Providers](./docs/providers/overview.md) — teach mgtt about your technology
- [Full Specification](./docs/specs.md) — the v1.0 spec
- [Documentation site](https://mgt-tool.github.io/mgtt) — browsable docs

## For TLA+ users

If you've written a TLA+ spec, mgtt will feel familiar — it borrows the same philosophy and applies it to operational troubleshooting rather than protocol verification.

**What carries over:**

- **Model-first, before the system exists.** In TLA+ you write the spec and run TLC before writing code. In mgtt you write `provider.yaml` + `system.model.yaml` and run `mgtt simulate` against scenarios before production hits them. Both tools monetize "think before you act."
- **States, transitions, invariants.** A mgtt type's state machine (`states.when` guards, first-match-wins) is the same shape as a TLA+ state + action ontology. `healthy:` is the per-component invariant.
- **Unresolved as a first-class answer.** TLC treats unexplored branches honestly; mgtt's `UnresolvedError` propagates through expression evaluation so a missing fact short-circuits to "unknown", never coerces to a lie.
- **Counterexamples, not assertions.** TLC hands you a concrete violating trace; `mgtt plan` hands you a concrete probe-by-probe narrowing — "probed A (healthy, eliminated), probed B (healthy, eliminated), root cause: C." Evidence, not claims.

**What's different:**

- **Runtime, not just symbolic.** mgtt mixes the declarative model with live observation. That's closer to runtime verification than to classical model checking.
- **Cost-ordered diagnosis, not exhaustive exploration.** mgtt walks a dependency graph and eliminates branches based on observed facts, ranked by probe cost. TLC explores the full reachable state space; mgtt finds the cheapest path to a root cause.
- **Operational concerns are first-class.** Probe cost, cache TTLs, read-only auth scope — things Lamport would rightly consider below the spec layer — sit inside the mgtt vocabulary because operators need them at 3am.

If you squint, mgtt is "TLA+ for oncall": declare-first-then-check, invariants-and-states ontology, but trading exhaustive proof for a ranked live-probe plan that fits in a terminal.

## License

MIT
