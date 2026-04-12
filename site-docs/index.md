# MGTT — Model Guided Troubleshooting Tool

Encode your system's dependencies once. When something breaks, the engine tells you what to check next — eliminating healthy components, narrowing the search, finding root cause in minutes.

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

4 components probed, 2 eliminated, root cause found. You didn't need to know the system — the model knew it for you.

---

## What mgtt gives you

1. **Model once** — describe components, dependencies, and what "healthy" means in YAML
2. **Simulate in CI** — inject synthetic failures, assert the engine reasons correctly, catch model gaps before production
3. **Troubleshoot at 3am** — press Y, the engine picks the most informative probe at every step, root cause in minutes

## Get started

- [Install](getting-started/install.md)
- [Quick Start](getting-started/quickstart.md) — model, validate, simulate in 5 minutes
- [Simulation walkthrough](concepts/simulation.md) — design-time model validation
- [Troubleshooting walkthrough](concepts/troubleshooting.md) — runtime incident response
- [Writing Providers](providers/overview.md) — teach mgtt about your technology
- [CLI Reference](reference/cli.md) — every command
- [Provider Registry](reference/registry.md) — community providers
- [Full Specification](reference/spec.md) — the v1.0 spec
