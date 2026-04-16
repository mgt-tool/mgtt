![mgtt](images/mgtt_full_lockup.png)

#

If you build or maintain anything with more than two components — a web app with a frontend, an API, and a database; a set of microservices behind a load balancer; a data pipeline with queues, workers, and storage — you know the drill:

Something stops working. You check the logs. Nothing obvious. You check the database. Looks fine. You check the API. Restarting. Why? You check the config. You check the deploy history. You ask the person who wrote it. They're asleep. You open three terminals and start guessing.

**The core problem:** troubleshooting distributed systems is slow, unstructured, and depends entirely on whoever happens to know the system. There's no map, no systematic narrowing, no way to know what you've already ruled out.

**mgtt fixes this.** You describe your system's dependencies once in a YAML model. When something breaks, a constraint engine walks the dependency graph, probes components in order of information value, and eliminates healthy branches. It always knows what to check next and why.

An SRE can drive the loop manually (press Y at each step). An AI agent can drive it autonomously via the same interface — mgtt is designed to be equally useful to humans and LLMs. The engine reasons; whoever's on call executes.

## On this page

- [See it in action](#see-it-in-action) — simulation + troubleshooting demos
- [What mgtt gives you](#what-mgtt-gives-you)
- [Get started](#get-started) — install + quickstart
- [Learn](#learn) — concepts + simulation + troubleshooting walkthroughs
- [Reference](#reference) — schemas, CLI, configuration, registry
- [Extend](#extend) — write your own provider

## See it in action

### Simulation: catch model gaps in CI

Write a scenario: "if rds goes down and api crash-loops, the engine should blame rds, not api."

```
$ mgtt simulate --all

  rds unavailable                          ✓ passed
  api crash-loop independent of rds        ✓ passed
  frontend crash-looping, api healthy      ✓ passed
  all components healthy                   ✓ passed

  4/4 scenarios passed
```

No running system. No credentials. Runs on every PR.

[Full simulation walkthrough](concepts/simulation.md)

### Troubleshooting: root cause in 6 probes

Monday 3am. Alert fires. You run `mgtt plan` and press Y:

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

4 components probed, 2 eliminated, root cause found. You didn't need to know the system — the model knew it for you. An AI agent could run this same loop autonomously.

[Full troubleshooting walkthrough](concepts/troubleshooting.md)

---

## What mgtt gives you

1. **Model once** — describe components, dependencies, and what "healthy" means in YAML
2. **Simulate in CI** — inject synthetic failures, assert the engine reasons correctly, catch model gaps before production
3. **Troubleshoot at 3am** — press Y (or let an AI agent drive), the engine picks the most informative probe at every step

| | Design time | At 3am |
|---|---|---|
| Command | `mgtt simulate` | `mgtt plan` |
| Facts from | Scenario YAML | Real probes (kubectl, aws) |
| Driven by | CI pipeline | SRE or AI agent |
| Output | Pass/fail assertions | Guided root cause |

## Get started

- [Quick Start](getting-started/quickstart.md) — complete end-to-end example: model, scenarios, simulate
- [Install](getting-started/install.md) — one-liner, Go, Docker, from source

## Learn

- [How It Works](concepts/how-it-works.md) — the constraint engine and dependency graph
- [Simulation walkthrough](concepts/simulation.md) — design-time model validation
- [Troubleshooting walkthrough](concepts/troubleshooting.md) — runtime incident response

## Reference

- [Model Schema](reference/model-schema.md) — every field in `system.model.yaml`
- [Scenario Schema](reference/scenario-schema.md) — every field in scenario files
- [Type Catalog](reference/type-catalog.md) — all provider types, facts, states, and failure modes
- [CLI Reference](reference/cli.md) — every command
- [Provider Registry](reference/registry.md) — official and community providers
- [Full Specification](reference/spec.md) — the v1.0 spec

## Extend

- [Writing Providers](providers/overview.md) — teach mgtt about your technology
