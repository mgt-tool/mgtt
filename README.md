# MGTT — Model Guided Troubleshooting Tool

Encode your system's dependencies once. When something breaks, the engine tells you what to check next — eliminating healthy components, narrowing the search, finding root cause in minutes instead of hours.

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

No running system. No credentials. Runs on every PR. If someone removes a dependency from the model, the scenario fails and the PR is blocked.

[Full simulation walkthrough](./docs/simulation-scenario.md)

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
     cost: low | AWS API read-only | eliminates PATH C if healthy

  ✓ rds.available = true   ✓ healthy       ← eliminated

  -> probe frontend ready_replicas
     cost: low | kubectl read-only | eliminates PATH A if healthy

  ✓ frontend.ready_replicas = 2   ✓ healthy  ← eliminated

  Root cause: api
  Path:       nginx <- api
  State:      degraded
  Eliminated: frontend, rds
```

The engine probed 4 components, eliminated 2 (rds healthy, frontend healthy), and found the root cause: api is crash-looping. You didn't need to know the system — the model knew it for you.

[Full troubleshooting walkthrough](./docs/troubleshooting-scenario.md)

---

## TL;DR

**The problem:** troubleshooting distributed systems is slow, unstructured, and depends on whoever happens to know the system.

**What mgtt does:**

1. You describe your system once in a YAML model (components, dependencies, what "healthy" means)
2. A constraint engine walks the dependency graph, probing components and eliminating healthy branches
3. Each probe is ranked by how much it narrows the search — the engine always picks the most informative, cheapest check next

**Two modes, same model:**

| | Design time | At 3am |
|---|---|---|
| Command | `mgtt simulate` | `mgtt plan` |
| Facts from | Scenario YAML | Real probes (kubectl, aws) |
| Needs | Nothing | Environment credentials |
| Output | Pass/fail assertions | Guided root cause |

---

## Install

```bash
# One-liner
curl -sSL https://raw.githubusercontent.com/sajonaro/mgtt/main/install.sh | sh

# Or via Go
go install github.com/sajonaro/mgtt/cmd/mgtt@latest

# Or via Docker
docker compose run --rm mgtt version
```

## Quick start

```bash
mgtt init                          # scaffold system.model.yaml
# edit it with your components and dependencies
mgtt model validate                # check the model
mgtt provider install kubernetes   # install providers
mgtt simulate --all                # run failure scenarios
mgtt plan                          # troubleshoot a live system
```

---

## The model

```yaml
# system.model.yaml
meta:
  name: storefront
  version: "1.0"
  providers:
    - kubernetes
  vars:
    namespace: production

components:
  nginx:
    type: ingress
    depends:
      - on: frontend
      - on: api

  frontend:
    type: deployment
    depends:
      - on: api

  api:
    type: deployment
    depends:
      - on: rds

  rds:
    providers:
      - aws
    type: rds_instance
    healthy:
      - connection_count < 500
```

The model is the single source of truth. It's version-controlled alongside your infrastructure code.

## Providers

Providers teach mgtt about technologies. Each provider defines component types, observable facts, states, and failure modes — all in mgtt's vocabulary.

```
$ mgtt provider ls

  ✓ aws         v0.1.0  AWS resources
  ✓ kubernetes  v1.0.0  Kubernetes workloads
```

Install from GitHub:

```bash
mgtt provider install https://github.com/sajonaro/mgtt-provider-docker
```

[Write your own provider](./providers/README.md) — three things: a YAML vocabulary, a binary that probes, an install hook.

## How it works

```mermaid
graph LR
  internet([internet]) --> nginx
  nginx[nginx\nreverse proxy] --> frontend
  nginx --> api
  frontend[frontend\nReact SPA] --> api
  api[api\nNode.js] --> rds[(rds\nAWS RDS)]

  style nginx     fill:#E1F5EE,stroke:#0F6E56,color:#085041
  style frontend  fill:#E1F5EE,stroke:#0F6E56,color:#085041
  style api       fill:#E1F5EE,stroke:#0F6E56,color:#085041
  style rds       fill:#E6F1FB,stroke:#185FA5,color:#0C447C
  style internet  fill:#F1EFE8,stroke:#5F5E5A,color:#444441
```

The constraint engine starts from the outermost component and works inward. At each step it picks the probe that eliminates the most failure paths for the lowest cost. Healthy components are eliminated. The tree shrinks until one path remains — that's your root cause.

The engine is pure: no I/O, no credentials, no side effects. The same engine powers `mgtt simulate` (injected facts) and `mgtt plan` (real probes). Providers handle the actual probing.

![Troubleshooting mode](./docs/images/mgtt_trouble.png)

![Simulation mode](./docs/images/mgtt_sim.png)

---

## More

- [Simulation walkthrough](./docs/simulation-scenario.md) — design-time model validation
- [Troubleshooting walkthrough](./docs/troubleshooting-scenario.md) — runtime incident response
- [Writing providers](./providers/README.md) — teach mgtt about your technology
- [CLI reference](#cli-reference) — every command
- [Full specification](./docs/specs.md) — the v1.0 spec
- [Documentation site](https://sajonaro.github.io/mgtt) — browsable docs

## CLI reference

```
mgtt init                              Scaffold system.model.yaml
mgtt model validate [path]             Validate model

mgtt provider install <name|path|url>  Install a provider
mgtt provider ls                       List providers
mgtt provider inspect <name> [type]    Inspect types

mgtt simulate --all                    Run all scenarios
mgtt simulate --scenario <file>        Run one scenario

mgtt incident start [--id ID]          Start incident
mgtt incident end                      Close incident

mgtt plan [--component NAME]           Guided troubleshooting
mgtt fact add <c> <k> <v> [--note ..]  Add observation

mgtt ls                                Components and status
mgtt ls facts [component]              Collected facts
mgtt status                            Health summary

mgtt stdlib ls                         Primitive types
mgtt stdlib inspect <type>             Type definition
```

## Design principles

- **Zero cognitive load at incident time.** The on-call engineer presses Y.
- **Engine is pure.** No I/O, no credentials. Same engine for CLI, simulation, and AI.
- **Credentials belong to the environment.** Same model as Terraform.
- **State is observed, not declared.** Component states derive from facts automatically.
- **Guided, not automated.** mgtt tells you what to check next. You decide whether to check it.

## License

MIT
