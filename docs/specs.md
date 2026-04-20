# `mgtt` — **m**odel **g**uided **t**roubleshooting **t**ool
## Specification v{{ MGTT_VERSION }}

This page is the formal spec index. Most sections now live in the reference pages — follow the links. Two sections that don't fit elsewhere (**MCP Service** and **Design Principles**) are inlined below.

## On this page

- [What mgtt is](#what-mgtt-is)
- [Reference index](#reference-index) — model / facts / providers / engine / CLI
- [MCP Service](#mcp-service) — tools, schemas, autonomy modes
- [Design Principles](#design-principles)

---

## What mgtt is

`mgtt` lets you encode a system model once, accumulate timestamped observations into a fact store, and use constraint propagation over the model and facts to guide — not replace — the troubleshooting process.

- **Not** a monitoring tool. It doesn't run continuously.
- **Not** an automation tool. It doesn't fix things.
- **Not** AI-dependent. AI is one possible consumer of the model, not a requirement.
- **Not** system-specific. The model language works for any distributed system.

The closest analogy is Terraform: separate desired state (model) from observed state (facts), and reason over the diff. `mgtt` does this for understanding, not provisioning.

```
model author       writes system.model.yaml once, calmly, knows the system
on-call engineer   mgtt incident start
                   mgtt diagnose
                   [structured root-cause report]
                   mgtt incident end
```

Cognitive load belongs at authoring time, not incident time.

Implementation: Go. Module path `github.com/mgt-tool/mgtt`. Binary: `mgtt`.

---

## Reference index

Every schema, format, and CLI surface lives in a reference page:

| Topic | Reference |
|---|---|
| Model file — `system.model.yaml` | [Model Schema](reference/model-schema.md) |
| Hand-authored scenario files — `scenarios/*.yaml` | [Scenario Schema](reference/scenario-schema.md) |
| Generated chain sidecar — `scenarios.yaml` | [scenarios.yaml Reference](reference/scenarios-yaml.md) |
| Provider manifest — `manifest.yaml` | [Manifest Schema](reference/manifest.md) |
| Stdlib primitives + provider-contributed types | [Type Catalog](reference/type-catalog.md) |
| How the engine picks probes and terminates | [Engine Reference](reference/engine.md) |
| Every CLI subcommand and flag | [CLI Reference](reference/cli.md) |
| Runtime configuration — `$MGTT_HOME`, env vars | [Configuration](reference/configuration.md) |
| Provider image capabilities — `needs:` vocabulary | [Image Capabilities](reference/image-capabilities.md) |
| Provider registry format | [Registry Reference](reference/registry.md) |

Conceptual overviews — read these before the reference pages if you're new:

- [How It Works](concepts/how-it-works.md) — model, facts, providers, the engine's place
- [Simulation](concepts/simulation.md) — model drift detection, regression harness, design-time validation
- [Troubleshooting](concepts/troubleshooting.md) — the 3am flagship workflow

Provider authors — start here instead:

- [Writing Providers — Overview](providers/overview.md)
- [Vocabulary](providers/vocabulary.md) — facts, states, failure modes, healthy rules
- [Binary Protocol](providers/protocol.md) — probe argv, exit codes, error classes

---

## MCP Service

`mgtt` exposes its constraint engine as an MCP service, callable by LLMs and AI agents. CLI and MCP are equal consumers.

### Tools

```
mgtt://tools/plan          run constraint engine, return failure path tree
mgtt://tools/probe         run a probe, append fact, return updated tree
mgtt://tools/fact/add      add a manual fact, return updated tree
mgtt://tools/ls/components list components and current status
mgtt://tools/ls/facts      list facts for a component
```

### Tool Schemas

**plan**
```json
{
  "input": {
    "component":  "string (optional — defaults to outermost)",
    "from_fact":  "string (optional — e.g. 'error_rate=0.94')"
  },
  "output": {
    "incident":    "string",
    "entry_point": "string",
    "state":       "string",
    "paths": [{
      "id":         "string",
      "components": ["string"],
      "hypothesis": "string",
      "eliminated": "boolean",
      "reason":     "string (if eliminated)"
    }],
    "suggested_probe": {
      "component":  "string",
      "fact":       "string",
      "eliminates": ["string"],
      "cost":       "low | medium | high",
      "access":     "string",
      "command":    "string"
    }
  }
}
```

**probe**
```json
{
  "input": {
    "component": "string",
    "fact":      "string (optional — all facts if omitted)"
  },
  "output": {
    "fact":              "string",
    "value":             "any",
    "collector":         "string",
    "at":                "ISO8601",
    "paths_remaining":   "integer",
    "paths_eliminated":  "integer",
    "updated_plan":      "plan output (full)"
  }
}
```

**fact/add**
```json
{
  "input": {
    "component": "string",
    "key":       "string",
    "value":     "any",
    "at":        "ISO8601 (optional)",
    "note":      "string (optional)"
  },
  "output": {
    "appended":      "boolean",
    "updated_plan":  "plan output (full)"
  }
}
```

### Autonomy Modes

```
observe       AI sees facts and paths, surfaces to human
              never calls probe or fact/add autonomously

assist        AI runs probe when cost == low AND access is read-only
              surfaces to human for cost == medium|high or write
              default mode

autonomous    AI drives the full loop, human gets report at end
              not recommended for production systems
```

---

## Design Principles

- **Zero cognitive load at incident time.** The on-call engineer runs `mgtt diagnose` and reads a structured report. All system knowledge lives in the model, authored calmly beforehand.
- **Simple until explicit.** Defaults cover 90% of cases. Namespacing and overrides exist for the other 10%.
- **Pecking order is the single resolution rule.** Type, facts, probes, data types all resolve the same way: first provider wins.
- **State is observed, not declared.** Component states derive from facts automatically. Engineers never set or advance state.
- **Stdlib is primitives only.** Higher-level types belong in providers.
- **Credentials belong to the environment.** `mgtt` never stores, manages, or transmits credentials. Same model as Terraform.
- **Providers are self-contained and external.** Each provider lives in its own git repository. Providers depend only on the mgtt provider SDK.
- **AI friendly, not AI dependent.** MCP makes `mgtt` callable by any LLM. The constraint engine reasons — the AI drives the loop.
- **Append only.** The fact store is a record, not a scratchpad.
- **Derive, don't persist.** State machine and failure path tree computed fresh. Only observations and current position stored.
- **Engine is pure.** The constraint engine has no I/O, no probe execution, no credential access, no filesystem operations. It takes a model, providers, and facts as input and returns a failure path tree. The same engine is callable from the CLI, simulation runner, and MCP service — only the source of facts differs.
- **Guided, not automated.** `mgtt` tells you what to check next and why. Human or AI decides whether to check it.
