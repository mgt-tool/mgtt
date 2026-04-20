![mgtt](images/mgtt_full_lockup.png)

When something breaks in a distributed system — and the person who built it is asleep — you open three terminals and start guessing. **mgtt replaces the guessing with a constraint engine.**

<div class="approach" markdown>

- **Describe once.** Your system's dependencies in a single YAML model.
- **Walk the graph.** At 3am, the engine probes components in order of information value and eliminates healthy branches.
- **Know what's next.** Always — and why.

</div>

Press Y at each step yourself, or hand the loop to an AI agent — same interface either way.

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

> **4 components probed. 2 eliminated. Root cause named.** The engine ranks probes by information value, so every call moves the answer forward. You didn't need to know the system — the model knew it for you. Partial visibility (RBAC refusals, transient throttles) surfaces as a visible flag in the report rather than aborting the session.

Failure chains are pre-enumerated into a committed `scenarios.yaml` at design time, so diagnose eliminates whole branches before running a probe.

[Full troubleshooting walkthrough](concepts/troubleshooting.md) | [`mgtt diagnose` reference](concepts/troubleshooting.md#autopilot-mode-mgtt-diagnose) | [scenarios.yaml](reference/scenarios-yaml.md)

### Simulation in CI: catch model drift before it matters (`mgtt simulate`)

Before the system is even running, `mgtt simulate` verifies the model's reasoning with hand-authored scenarios — a tiny YAML assertion like *"if rds goes down and api crash-loops, the engine should blame rds, not api"* — and asserts the engine concludes the same thing. No live system, no credentials, no cluster access. Runs anywhere Go runs.

Wire it into every PR for:

- **Model drift detection** — when the real system evolves (new services, renamed components, changed dependencies), a stale model silently drifts away from reality. A failing scenario tells you *before* the model is needed at 3am.
- **Architecture unit tests** — each scenario is a declarative assertion. Refactor the model, break a conclusion, the suite fails. Safe renames, safe dependency moves.
- **Design-time validation** — write the model before the system exists; reason about dependency holes before building them. The engine treats your design as executable logic.
- **Regression harness** — the next time a real incident happens, encode it as a scenario. The engine must now identify that chain forever. Your postmortems become tests.

```
$ mgtt simulate --all

  rds unavailable                          ✓ passed
  api crash-loop independent of rds        ✓ passed
  frontend crash-looping, api healthy      ✓ passed
  all components healthy                   ✓ passed

  4/4 scenarios passed
```

[Full simulation walkthrough](concepts/simulation.md)

---

## What mgtt gives you

One model, two moments:

- **Model once** — describe components, dependencies, and what "healthy" means in YAML.
- **Simulate in CI** — inject synthetic failures; assert the engine reasons correctly; catch model drift before it matters.
- **Troubleshoot at 3am** — `mgtt diagnose` gives you a structured root-cause report; run it from CI, Slack, an AI agent, or your laptop.

|             | Design time     | At 3am                                               |
|-------------|-----------------|------------------------------------------------------|
| Command     | `mgtt simulate` | `mgtt diagnose`                                      |
| Facts from  | scenario YAML   | real probes                                          |
| Driven by   | CI pipeline     | on-call engineer, CI job, Slack bot, or AI agent     |
| Output      | pass/fail       | root cause + chain + eliminated components           |

`mgtt plan` exists too — same engine, interactive press-Y mode for debugging models or teaching. Not the daily driver.

## Get started

- [Quick Start](getting-started/quickstart.md) — complete end-to-end example: model, scenarios, simulate
- [Install](getting-started/install.md) — one-liner, Go, Docker, from source

## Learn

- [How It Works](concepts/how-it-works.md) — the constraint engine and dependency graph
- [Simulation walkthrough](concepts/simulation.md) — design-time model validation
- [Troubleshooting walkthrough](concepts/troubleshooting.md) — runtime incident response

## Working with Providers

- [Using Providers](concepts/using-providers.md) — how mgtt invokes providers at probe time
- [Install Methods](concepts/provider-install-methods.md) — git build vs. pre-built Docker image
- [Names and Versions](concepts/provider-fqn-and-versions.md) — FQN + version constraint resolution
- [Provider Capabilities](reference/image-capabilities.md) — `needs:` vocabulary and operator overrides

## Provider Registry

- [Official and community providers](reference/registry.md) — browse what's available, copy the install line

## Reference

- [Model Schema](reference/model-schema.md) — every field in `system.model.yaml`
- [Scenario Schema](reference/scenario-schema.md) — hand-authored `scenarios/*.yaml` for `mgtt simulate`
- [`scenarios.yaml`](reference/scenarios-yaml.md) — the generated sidecar `mgtt diagnose` consumes
- [Type Catalog](reference/type-catalog.md) — all provider types, facts, states, and failure modes
- [CLI Reference](reference/cli.md) — every command
- [Full Specification](reference/spec.md) — the v1.0 spec

## Extend

- [Writing Providers](providers/overview.md) — teach mgtt about your technology
