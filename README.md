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

**mgtt is for troubleshooting live production, not proving protocols correct.** If you've written a TLA+ spec, the philosophy will feel familiar — but the job is different. TLA+ asks "is my design correct under all interleavings?" mgtt asks "my pager just went off, which of my running components is actually broken?"

The parallel that helps: you write the spec of your system (types, states, how failures propagate) once, and *that same spec* drives two things — simulation in CI (like TLC, but against scenario injections) and live diagnosis at 3am (walking the dependency graph, probing real kubectl/AWS/docker, eliminating healthy branches). The model is the source of truth; the engine turns it into either a proof of design or a root-cause.

Where the analogy ends: mgtt doesn't do exhaustive state-space exploration — it does cost-ordered live probing. Every step observes real infrastructure; every probe you *didn't* need to run is information saved. Operational concerns (probe cost, TTLs, read-only auth scope) are first-class vocabulary because the goal is to be useful in the terminal where the incident is happening, not to hand an engineer a 300-state counterexample trace at 3am.

Call it "a spec that also runs a diagnostic plan against your cluster." If TLA+ is design-time correctness, mgtt is run-time localization.

## License

MIT
