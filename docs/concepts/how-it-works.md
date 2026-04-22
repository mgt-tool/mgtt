# How It Works

mgtt encodes your system's dependency graph in a YAML model. A constraint engine walks the graph, probing components and eliminating healthy branches until one failure path remains.

The same model and engine serve two phases — the only difference is where facts come from.

## Architecture at a glance

```mermaid
%%{init: {'flowchart': {'defaultRenderer': 'elk'}}}%%
flowchart LR
    Human["human operator"]
    Agent["LLM agent<br/>(Claude Code, CI bot)"]

    subgraph MGTT["mgtt core &nbsp; · &nbsp; pure reasoning · no backend credentials"]
        direction TB
        CLI["CLI<br/>simulate · diagnose · plan"]
        MCP["MCP server<br/>stdio · HTTP + bearer"]
        Engine["constraint engine<br/>walk · rank · eliminate"]
        Model[("system.model.yaml<br/>components · dependencies")]
        Scenarios[("scenarios.yaml<br/>scripted failures")]
        State[("incident state<br/>$MGTT_HOME/incidents/")]
        CLI --> Engine
        MCP --> Engine
        Model --> Engine
        Scenarios -.->|simulate mode| Engine
        Engine --> State
    end

    subgraph ADAPTERS["adapters (providers) &nbsp; · &nbsp; the only place backend credentials live"]
        direction TB
        K8s["kubernetes<br/>kubectl"]
        AWS["aws<br/>aws-cli"]
        Docker["docker<br/>docker CLI"]
        Terra["terraform<br/>state"]
        Tempo["tempo<br/>TraceQL"]
        Qwit["quickwit<br/>search API"]
    end

    subgraph REG["registry"]
        direction TB
        Remote["registry.yaml<br/>GitHub Pages · SemVer tags"]
        Local["$MGTT_HOME/providers/<br/>installed + verified"]
        Remote -->|mgtt provider install| Local
    end

    subgraph SUT["system under test &nbsp; · &nbsp; your production, not touched by mgtt core"]
        direction TB
        NGX["nginx ingress"]
        API["api replicas<br/>(k8s deployment)"]
        Cache["redis"]
        Queue["kafka / sqs"]
        RDS[("postgres / rds")]
        Tracing["tempo + quickwit<br/>(spans · logs)"]
        NGX --> API
        API --> RDS
        API --> Cache
        API --> Queue
        API -.->|emits traces| Tracing
    end

    Human --> CLI
    Agent --> MCP
    Engine ==>|probe request| ADAPTERS
    ADAPTERS ==>|shell / SDK call| SUT
    SUT ==>|stdout · JSON · SQL rows| ADAPTERS
    ADAPTERS ==>|parsed facts| Engine
    Local -.->|loaded at startup| ADAPTERS
```

Four things, four boundaries:

- **mgtt core** — the engine, the model, the scenarios, the incident store. Pure reasoning. Never opens a socket, never reads a credential. Two entry points: the CLI for humans and CI, the MCP server for LLM agents (same engine underneath).
- **Adapters** (providers) — plugins that cross the trust boundary. Each adapter knows one backend's command-line or SDK and how to parse its output into typed facts. Credentials, RBAC, IAM — all live here, never in mgtt core.
- **Registry** — the published index (`registry.yaml`, served off GitHub Pages, SemVer-tagged). `mgtt provider install` resolves a ref, fetches the adapter (git source or docker image), verifies the manifest, and lands it under `$MGTT_HOME/providers/` where the runtime discovers it.
- **System under test** — the real production thing your model claims to describe. Ingress, app replicas, caches, queues, databases, tracing backends. Adapters touch it; mgtt core is strictly upstream.

Two fact sources, same engine:

- **Simulation mode** — `scenarios.yaml` feeds synthetic facts in CI. No adapter runs, the SUT isn't contacted, the test asserts the engine's conclusion.
- **Diagnosis mode** — adapters run real probes against the SUT and append facts as they land. The engine re-plans after each one, narrowing the path tree until a root cause emerges.

Operators — human or LLM — only ever talk to CLI / MCP. Never directly to an adapter, never to the SUT. That boundary is what makes the engine safe to hand to an agent on a CI runner.

## On this page

- [Architecture at a glance](#architecture-at-a-glance) — mgtt core, adapters, registry, system under test
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

The engine is pure — no I/O, no credentials, no side effects. The same engine powers both `mgtt simulate` and `mgtt diagnose`. Only the source of facts differs.

For the full internals (strategies, probe-selection heuristics, termination conditions, complexity math), see the **[Engine Reference](../reference/engine.md)**. This page stays at concept level.

## Two modes, same model

| | Simulation | Troubleshooting |
|---|---|---|
| Command | `mgtt simulate` | `mgtt diagnose` |
| Facts from | Scenario YAML (authored) | Live probes via installed providers |
| Needs | Nothing | Environment credentials |
| Runs in | CI pipeline | On-call laptop, CI job, Slack bot, or AI agent |
| Output | Pass/fail assertions | Structured root-cause report |

### Simulation (`mgtt simulate`)

You author scenario files that inject synthetic facts. The engine reasons over them and you assert the conclusion. This tests the **model's reasoning**, not the system's behavior.

```
model.yaml + scenario.yaml → engine → pass/fail
```

If someone removes a dependency from the model, the scenario fails. The PR is blocked. The blind spot never reaches production.

[Full simulation walkthrough →](simulation.md)

### Troubleshooting (`mgtt diagnose`)

The engine walks the dependency graph from the outermost component inward. At each step it picks the single highest-value, lowest-cost probe, runs it, and continues until one failure path remains or the probe budget is hit.

```
model.yaml + live probes → engine → root cause
```

`mgtt plan` is the interactive press-Y variant for debugging models or teaching — same engine, prompts at each step.

[Full troubleshooting walkthrough →](troubleshooting.md)

## Probe ranking

Not all probes are equal. The engine ranks each candidate by:

1. **Information value** — how many failure paths does this probe eliminate?
2. **Cost** — how expensive/slow is this probe? (low/medium/high, declared by the provider)
3. **Access** — what credentials or permissions does it need?

The engine always suggests the probe that eliminates the most uncertainty for the least cost. See [Engine Reference — Probe selection heuristics](../reference/engine.md#probe-selection-heuristics) for the exact algorithm.

## Providers

Providers teach mgtt about technologies. Each provider defines:

- **Types** — component types (e.g., `deployment`, `rds_instance`)
- **Facts** — observable properties per type (e.g., `ready_replicas`, `available`)
- **States** — derived from facts (e.g., `live`, `degraded`, `stopped`)
- **Failure modes** — what downstream effects each non-healthy state can cause
- **Probes** — the actual commands to collect facts from live systems

Providers for each technology are installed separately. See the [Provider Registry](../reference/registry.md) for the current catalog — Kubernetes, AWS, Docker, Terraform, Tempo, Quickwit, and anything else the community has authored. Writing your own is a [standalone guide](../providers/overview.md).

[Provider Type Catalog →](../reference/type-catalog.md) | [Writing Providers →](../providers/overview.md)
