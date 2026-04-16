# How It Works

mgtt encodes your system's dependency graph in a YAML model. A constraint engine walks the graph, probing components and eliminating healthy branches until one failure path remains.

The same model and engine serve two phases — the only difference is where facts come from.

## On this page

- [The three artifacts](#the-three-artifacts) — model, facts, providers
- [The constraint engine](#the-constraint-engine) — how reasoning narrows the search
- [Two modes, same model](#two-modes-same-model) — design-time vs runtime
- [Probe ranking](#probe-ranking) — what to check next, and why
- [Providers](#providers) — where backend knowledge lives

---

## The three artifacts

```
system.model.yaml       you write once, version controlled
system.state.yaml       mgtt writes during incidents, append-only
providers/              community plugins, one per technology
```

The model describes the system. The state file records observations. Providers supply the vocabulary (types, facts, states) and the commands to collect facts from live systems.

## The constraint engine

The engine is mgtt's core. It takes four inputs:

1. **Components** — from the model
2. **Failure modes** — from the providers
3. **Propagation rules** — from the dependency graph
4. **Current facts** — from scenarios (simulation) or live probes (troubleshooting)

It produces a **ranked failure path tree**: which paths are still possible, which are eliminated, and which single probe would narrow the search the most.

The engine is pure — no I/O, no credentials, no side effects. The same engine powers both `mgtt simulate` and `mgtt plan`. Only the source of facts differs.

## Two modes, same model

| | Simulation | Troubleshooting |
|---|---|---|
| Command | `mgtt simulate` | `mgtt plan` |
| Facts from | Scenario YAML (authored) | Live probes (kubectl, aws) |
| Needs | Nothing | Environment credentials |
| Runs in | CI pipeline | On-call engineer's laptop |
| Output | Pass/fail assertions | Guided root cause |
| Driven by | Automation | SRE or AI agent |

### Simulation (`mgtt simulate`)

You author scenario files that inject synthetic facts. The engine reasons over them and you assert the conclusion. This tests the **model's reasoning**, not the system's behavior.

```
model.yaml + scenario.yaml → engine → pass/fail
```

If someone removes a dependency from the model, the scenario fails. The PR is blocked. The blind spot never reaches production.

[Full simulation walkthrough →](simulation.md)

### Troubleshooting (`mgtt plan`)

The engine walks the dependency graph from the outermost component inward. At each step it suggests the single highest-value, lowest-cost probe to run. You (or an AI agent) execute the probe and feed the result back.

```
model.yaml + live probes → engine → root cause
```

The loop repeats until one failure path remains — that's your root cause.

[Full troubleshooting walkthrough →](troubleshooting.md)

## Probe ranking

Not all probes are equal. The engine ranks each candidate by:

1. **Information value** — how many failure paths does this probe eliminate?
2. **Cost** — how expensive/slow is this probe? (low/medium/high, declared by the provider)
3. **Access** — what credentials or permissions does it need?

The engine always suggests the probe that eliminates the most uncertainty for the least cost.

## Providers

Providers teach mgtt about technologies. Each provider defines:

- **Types** — component types (e.g., `deployment`, `rds_instance`)
- **Facts** — observable properties per type (e.g., `ready_replicas`, `available`)
- **States** — derived from facts (e.g., `live`, `degraded`, `stopped`)
- **Failure modes** — what downstream effects each non-healthy state can cause
- **Probes** — the actual commands to collect facts from live systems

mgtt ships with official providers for Kubernetes and AWS. Community providers extend it to other technologies.

[Provider Type Catalog →](../reference/type-catalog.md) | [Writing Providers →](../providers/overview.md)
