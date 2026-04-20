![mgtt](images/mgtt_full_lockup.png)

When something breaks in a distributed system — and the person who built it is asleep — you open three terminals and start guessing. **mgtt replaces the guessing with a constraint engine.**

<div class="approach" markdown>

- **Describe once.** Your system's dependencies in a single YAML model.
- **Walk the graph.** At 3am, the engine probes components in order of information value and eliminates healthy branches.
- **Know what's next.** Always — and why.

</div>

Press Y at each step yourself, or hand the loop to an AI agent — same interface either way.

## See it in action

### Troubleshooting at 3am: root cause in 6 probes (`mgtt plan`)

This is mgtt's reason for being. Alert fires. You run `mgtt plan` and press Y:

```
$ mgtt plan

  -> probe nginx upstream_count
     cost: low | kubectl read-only

  ✓ nginx.upstream_count = 0   ✗ unhealthy

  -> probe api restart_count
     cost: low

  ✓ api.restart_count = 47   ✗ unhealthy

  -> probe rds available
     cost: low | AWS API read-only

  ✓ rds.available = true   ✓ healthy       ← eliminated

  -> probe frontend ready_replicas
     cost: low | kubectl read-only

  ✓ frontend.ready_replicas = 2   ✓ healthy  ← eliminated

  Root cause: api
  Path:       nginx <- api
  State:      degraded
  Eliminated: frontend, rds
```

> **4 components probed. 2 eliminated. Root cause found.** The engine ranked probes by information value, so every call moved the answer forward. You didn't need to know the system — the model knew it for you.

[Full troubleshooting walkthrough](concepts/troubleshooting.md)

### Autopilot: hand the loop over (`mgtt diagnose`)

Same engine, no prompts. Hand it a probe budget and a deadline; get a structured final report:

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

Ideal for scheduled GitLab/GitHub Actions runs, Slack bots, LLM agents, and anywhere you want unattended root-cause analysis. Failure chains are pre-enumerated into a committed `scenarios.yaml` at design time, so diagnose eliminates whole branches before running a probe. Partial visibility (RBAC refusals, transient throttles) surfaces as a visible flag in the report rather than an abort.

[`mgtt diagnose` reference](concepts/troubleshooting.md#autopilot-mode-mgtt-diagnose) | [scenarios.yaml](reference/scenarios-yaml.md)

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

One model, three moments:

- **Model once** — describe components, dependencies, and what "healthy" means in YAML.
- **Simulate in CI** — inject synthetic failures; assert the engine reasons correctly; catch model gaps before production.
- **Troubleshoot at 3am** — press Y (`mgtt plan`) or hand the loop over (`mgtt diagnose`); the engine picks the most informative probe at every step.

|             | Design time     | At 3am (interactive)         | At 3am (autopilot)          |
|-------------|-----------------|------------------------------|-----------------------------|
| Command     | `mgtt simulate` | `mgtt plan`                  | `mgtt diagnose`             |
| Facts from  | scenario YAML   | real probes + Y/n            | real probes, no prompts     |
| Driven by   | CI pipeline     | SRE                          | AI agent or unattended run  |
| Output      | pass/fail       | guided root cause            | final report + chain        |

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
