![mgtt](docs/images/mgtt_full_lockup.png)

## model guided troubleshooting tool

If you build or maintain anything with more than two components, you know the drill: something stops working, you open three terminals and start guessing.

**mgtt fixes this.** You describe your system's dependencies once in a YAML model. When something breaks, a constraint engine walks the dependency graph, probes components in order of information value, and eliminates healthy branches. It always knows what to check next and why.

And before the system even exists, you can simulate failures against the model to verify the reasoning is correct — like unit tests for your architecture.

## See it in action

### Troubleshooting at 3am: `mgtt diagnose`

This is mgtt's reason for being. Alert fires. You trigger `mgtt diagnose` — from a GitLab/GitHub Actions job, a Slack slash-command, an LLM agent, or your laptop — and get a structured report back:

```
$ mgtt diagnose --suspect api --max-probes 10

  ▶ probe nginx upstream_count       ✗ unhealthy
  ▶ probe api ready_replicas         ✗ unhealthy
  ▶ probe rds available              ✓ healthy  ← eliminated
  ▶ probe frontend ready_replicas    ✓ healthy  ← eliminated

  Root cause: api.degraded
  Chain:      nginx ← api
  Probes run: 4/10
```

4 components probed, 2 eliminated, root cause named. The engine ranks probes by information value, so every call moves the answer forward — you didn't need to know the system, the model knew it for you. Partial visibility (RBAC refusals, transient throttles) surfaces as a visible flag in the report rather than aborting the session.

If you want to *watch* the engine pick each probe — for debugging the model, teaching, or a demo — `mgtt plan` is the interactive press-Y variant on the same engine. See [Troubleshooting](./docs/concepts/troubleshooting.md) for both.

### Simulation in CI: catch model drift before it matters (`mgtt simulate`)

Before the system is even running, `mgtt simulate` verifies the model's reasoning with hand-authored scenarios (`"if rds goes down and api crash-loops, root cause is rds"`). Wire it into every PR:

- **Model drift detection** — when the real system evolves (new services, renamed components, changed dependencies), a stale model silently drifts away from reality. A failing scenario tells you *before* the model is needed at 3am.
- **Architecture unit tests** — each scenario is a tiny declarative assertion. Refactor the model, break a conclusion, the suite fails. Safe renames, safe dependency moves.
- **Design-time validation** — write the model before the system exists; reason about dependency holes before building them.
- **Regression harness** — encode real incidents as scenarios. The engine must now identify that chain forever. Your postmortems become tests.

```
$ mgtt simulate --all

  rds unavailable                          ✓ passed
  api crash-loop independent of rds        ✓ passed
  frontend crash-looping, api healthy      ✓ passed
  all components healthy                   ✓ passed

  4/4 scenarios passed
```

No running system, no credentials. Runs anywhere Go runs.

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
mgtt simulate --all                # run failure scenarios (in CI)
mgtt diagnose                      # troubleshoot a live system
```

**Two moments, one model:**

| | Design time | At 3am |
|---|---|---|
| Command | `mgtt simulate` | `mgtt diagnose` |
| Facts from | Scenario YAML | Real probes |
| Driven by | CI pipeline | On-call engineer, CI job, Slack bot, or AI agent |
| Output | Pass/fail | Root cause + chain + eliminated components |

`mgtt plan` exists too — same engine, interactive press-Y mode. Useful for debugging models, teaching, or watching the reasoning unfold. Not the daily driver.

---

## Documentation

- [Quick Start](./docs/getting-started/quickstart.md) — complete end-to-end example
- [How It Works](./docs/concepts/how-it-works.md) — the constraint engine and dependency graph
- [Simulation](./docs/concepts/simulation.md) — design-time model validation
- [Troubleshooting](./docs/concepts/troubleshooting.md) — runtime incident response
- [Multi-File Models](./docs/concepts/multi-file-models.md) — when one system needs more than one model file (steady-state vs deploy-moment, etc.)
- [Provider Install Methods](./docs/concepts/provider-install-methods.md) — git clone vs Docker image; both install paths live side by side
- [Provider Names and Versions](./docs/concepts/provider-fqn-and-versions.md) — FQN and version constraints eliminate provider drift
- [Model Schema](./docs/reference/model-schema.md) — every field in `system.model.yaml`
- [Scenario Schema](./docs/reference/scenario-schema.md) — hand-authored `scenarios/*.yaml` for `mgtt simulate`
- [`scenarios.yaml`](./docs/reference/scenarios-yaml.md) — the generated sidecar `mgtt diagnose` consumes
- [Type Catalog](./docs/reference/type-catalog.md) — all provider types, facts, and states
- [CLI Reference](./docs/reference/cli.md) — every command
- [Provider Registry](./docs/reference/registry.md) — official and community providers
- [Writing Providers](./docs/providers/overview.md) — teach mgtt about your technology
- [Full Specification](./docs/specs.md) — the v1.0 spec
- [Documentation site](https://mgt-tool.github.io/mgtt) — browsable docs

## For TLA+ users

TLA+ checks your design. mgtt checks your running system.

Same idea — write the spec, let the tool do the thinking — pointed at a different problem. When something breaks in production, mgtt walks your spec against the live cluster and tells you which component is actually broken.

## License

mgtt is dual-licensed:

- **Core engine and CLI** (everything outside `sdk/provider/`) — [GNU Affero General Public License v3.0](LICENSE). Deploy mgtt internally however you like, but if you run it as a hosted service that users interact with over a network, you must publish any modifications you've made.
- **Provider SDK** (`sdk/provider/`) — [Apache License 2.0](sdk/provider/LICENSE). Third-party providers link against this code; permissive licensing so authors can release providers under whatever licence they choose.

See [`sdk/provider/NOTICE`](sdk/provider/NOTICE) for the full rationale, and [`LICENSES/`](LICENSES/) for both licence texts side-by-side.
