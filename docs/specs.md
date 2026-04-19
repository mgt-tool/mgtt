# `mgtt` — **m**odel **g**uided **t**roubleshooting **t**ool
## Specification v{{ MGTT_VERSION }}

## On this page

1. [What `mgtt` Is](#1-what-mgtt-is)
2. [Core Concepts](#2-core-concepts)
3. [The Three Artifacts](#3-the-three-artifacts) — model, facts, providers
4. [The Stdlib](#4-the-stdlib) — primitive types and units
5. [The Constraint Engine](#5-the-constraint-engine) — how reasoning works
6. [Model Format](#6-model-format) — `system.model.yaml`
7. [Fact Store Format](#7-fact-store-format) — observation log
8. [Provider Format](#8-provider-format) — `manifest.yaml` schema
9. [Complete Provider Example](#9-complete-provider-example)
10. [Provider Authoring Toolchain](#10-provider-authoring-toolchain)
11. [Authentication and Probe Execution](#11-authentication-and-probe-execution)
12. [State Machine](#12-state-machine)
13. [MCP Service](#13-mcp-service)
14. [CLI](#14-cli)
15. [Simulation](#15-simulation)
16. [Design Principles](#16-design-principles)

---

## 1. What `mgtt` Is

`mgtt` lets you encode a system model once, accumulate timestamped observations
into a fact store, and use constraint propagation over the model and facts to
guide — not replace — the troubleshooting process.

It is not a monitoring tool. It does not run continuously.
It is not an automation tool. It does not fix things.
It is not AI-dependent. AI is one possible consumer of the model, not a requirement.
It is not system-specific. The model language works for any distributed system.

The closest analogy is Terraform: separate desired state (model) from observed
state (facts), and reason over the diff. `mgtt` does this for understanding, not
provisioning.

The core usage model:

```
model author       writes system.model.yaml once, calmly, knows the system
on-call engineer   mgtt incident start
                   mgtt plan
                   [Y/n] y   <- repeat until done
                   mgtt incident end
```

Cognitive load belongs at authoring time, not incident time.

Implementation: Go. Module path `github.com/mgt-tool/mgtt`. Binary: `mgtt`.

---

## 2. Core Concepts

### 2.1 Model
A YAML file describing the system: its components, their types, their
dependencies, and which providers own them. Written once by an engineer,
version-controlled alongside infrastructure code.

### 2.2 Fact Store
A YAML file of timestamped observations keyed by component. Append-only.
Written by `mgtt` during guided probing or manually via CLI. Scoped to an
incident session. Current system state is a fact like any other.

### 2.3 Provider
A plugin that owns one or more component types. Defines the vocabulary of
facts, data types, probes, states, failure modes, and healthy defaults for
its types. Providers live in their own git repositories, Helm-plugin style.

### 2.4 Stdlib
A built-in provider called `mgtt`, always available without import. Defines
primitive data types only — the lowest-level building blocks every provider
can use without declaring dependencies on each other.

### 2.5 Constraint Engine
`mgtt`'s core. Takes four inputs — components, failure modes, propagation rules,
and current facts — and produces a ranked failure path tree. Starts from the
outermost component and works inward. Guides the engineer toward the next most
discriminating probe. Reruns after every new fact until one path remains.

### 2.6 Plan
The output of the constraint engine at any point in time: remaining failure
paths, eliminated paths, and the suggested next probe with reasoning.

### 2.7 State Machine
Derived automatically from the model and observed facts — never authored,
never persisted, never manually advanced. `mgtt` computes component states
from facts as they are collected. State transitions are observed, not declared.
Only the current position is stored as a fact.

### 2.8 MCP Service
`mgtt` exposes its constraint engine as an MCP (Model Context Protocol) service.
LLMs and AI agents call `mgtt` tools directly, driving the same guided loop
a human drives via CLI. CLI and MCP are equal consumers of the same engine.

---

## 3. The Three Artifacts

```
system.model.yaml       engineer writes once, version controlled
system.state.yaml       mgtt writes, append only, scoped per incident
providers/              external repositories, one per technology
```

The state machine and failure path tree are derived on demand. Neither is
stored separately.

---

## 4. The Stdlib

The stdlib is a built-in provider named `mgtt`, always available without import.
It defines primitive data types only. Higher-level types (replicas, lag,
error_rate) belong in technology-specific providers built on these primitives.
This prevents semantic collision across the ecosystem.

### 4.1 Primitive Data Types

```yaml
mgtt stdlib data_types:

  int:          base: int    unit: ~          range: ~
  float:        base: float  unit: ~          range: ~
  bool:         base: bool   unit: ~          range: ~
  string:       base: string unit: ~          range: ~
  duration:     base: float  unit: ms|s|m|h|d range: 0..
  bytes:        base: int    unit: b|kb|mb|gb|tb range: 0..
  ratio:        base: float  unit: ~          range: 0.0..1.0
  percentage:   base: float  unit: ~          range: 0.0..100.0
  count:        base: int    unit: ~          range: 0..
  timestamp:    base: string unit: ISO8601    range: ~
```

### 4.2 Referencing Stdlib Types

```yaml
type: duration          # implicit stdlib reference
type: mgtt.duration     # explicit — identical, preferred in provider definitions
```

### 4.3 Type Resolution and Validation

The type system has two layers: stdlib primitives (always available) and
provider-defined types (built on stdlib). Both follow the same structure:

```
base:    the underlying primitive (int, float, bool, string)
unit:    valid suffixes (pipe-separated), or ~ for unitless
range:   min..max, min.., ..max, or ~ for unconstrained
default: suggested value (provider types only)
```

Resolution chain for a literal like `"5s"`:

```
expression literal   "5s"
  -> parser recognises unit suffix "s"
  -> looks up which type declares "s" as a valid unit
  -> finds mgtt.duration (base: float, unit: ms|s|m|h|d)
  -> parses "5" as float, attaches unit "s"
  -> value valid iff fact.type is mgtt.duration or a provider type
     whose base is mgtt.duration
```

Validation occurs at three points:

```
provider load     data_types.base must resolve to a stdlib primitive
                  default must satisfy its own unit and range constraints

model validate    expression literals must match the type declared on
                  the referenced fact

fact append       value type-checked against the fact's declared type
                  at collection time
```

Unit-suffixed literals in expressions (5s, 10mb) are only valid when the
referenced fact's type declares that unit. `model validate` checks this
statically.

In v1.0 providers reference only mgtt stdlib types — no cross-provider
type references.

---

## 5. The Constraint Engine

### 5.1 Four Inputs

```
variables:          components
                    declared in model.yaml

domain:             possible failure modes per component
                    from provider failure_modes block

structural rules:   how failures propagate and when
                    from depends + while conditions in model.yaml

observations:       known healthy or unhealthy facts
                    from the fact store — eliminates branches
```

Entry point — where traversal begins:

```
default:    outermost component in the dependency graph
            (the component nothing else depends on)

override:   --component flag — start from a known-bad component

pre-loaded: mgtt fact add before mgtt plan
            tree is partially pruned before the first probe
```

Degraded inputs:

```
no facts         ->   no elimination, all paths equally probable
no while conds   ->   paths exist but no activation rules
no failure modes ->   paths exist but no domain to reason over
no components    ->   nothing
```

### 5.2 What the Engine Computes

Constraint propagation over the dependency graph produces a failure path tree:
all sequences of component failures that could explain observed unhealthy facts,
given what is already known.

```
entry point: nginx (outermost)
probe result: nginx.upstream_count = 0  x

PATH A  nginx <- api
          api.state == degraded | draining
          -> probe: api.endpoints   cost: low

PATH B  nginx <- api <- rds
          rds.state == degraded | stopped
          -> follows if PATH A confirmed

PATH C  nginx <- frontend
          frontend.state == degraded | draining
          -> probe: frontend.endpoints   cost: low
```

### 5.3 Probe Ranking — Information Gain

```
maximise:   failure paths eliminated if probe returns healthy
minimise:   cost (time, access required, risk)
prefer:     facts closest to entry point first
```

### 5.4 The Guided Loop

```
entry point determined         ->   outermost component or override
constraint propagation         ->   computes reachable failure paths
fact store                     ->   eliminates known-healthy branches
mgtt suggests next probe       ->   highest information gain, lowest cost
probe runs                     ->   new fact appended to store
state machine updates          ->   component states derived from new facts
constraint propagation reruns  ->   tree shrinks
repeat                         ->   until one path remains -> root cause
```

State is observed continuously as facts arrive. The engineer never declares
or advances state manually.

### 5.5 Manual Facts

```bash
mgtt fact add nginx config_loaded true --note "checked manually"
```

Manual facts have identical elimination power to collected facts.

### 5.6 Failure Mode Vocabulary

`can_cause` values match against observed unhealthy facts, not free-text
symptoms. The engine uses them to determine which downstream components
could be affected by a given upstream failure state.

Standard vocabulary:

```
upstream_failure        component upstream becomes unreachable
connection_refused      TCP connection actively refused
timeout                 connection or query exceeds time limit
query_timeout           database query exceeds time limit
5xx_errors              HTTP 5xx responses
dns_failure             name resolution fails
auth_failure            authentication or authorisation rejected
resource_exhaustion     CPU, memory, or file descriptor limit reached
```

Providers may define additional values for technology-specific failure modes.

### 5.7 The Guidance Output

`mgtt plan` prints the incident id, entry point, observed system state,
remaining and eliminated failure paths with per-path hypothesis conditions,
and a single suggested next probe annotated with what it eliminates, its cost,
and the access it requires. A fallback `mgtt fact add` command is offered in
case the probe cannot be run automatically. The engineer confirms `[Y/n]` to
run it.

---

## 6. Model Format

```yaml
# system.model.yaml

meta:
  name:      storefront
  version:   1.0
  providers:
    - kubernetes
    - datadog

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
      - on:    vault
        while: vault.state == starting

  rds:
    providers:
      - aws
    type: rds_instance
    healthy:
      - connection_count < 500

  vault:
    type: deployment
```

### 6.1 Meta-Level Providers

```
meta.providers          ->   default for all components
component.providers     ->   override, applies to this component only
omitted at component    ->   inherits meta.providers
```

### 6.2 Component Fields

| field         | required | description                                      |
|---------------|----------|--------------------------------------------------|
| providers     | no       | overrides meta.providers for this component      |
| type          | yes      | component type, resolved against provider list   |
| resource      | no       | upstream resource id the provider probes; falls back to the component key when empty; supports `{key}` placeholders from `meta.vars` |
| depends       | no       | list of dependency declarations                  |
| healthy       | no       | overrides or extends provider healthy defaults   |
| failure_modes | no       | overrides or extends provider failure modes      |

### 6.3 Providers — Pecking Order and Resolution Rules

```
1. TYPE RESOLUTION
   Scan providers list in order.
   First provider that declares the given type wins.
   That provider owns: schema, facts, data types, healthy, states, failure_modes.
   If no provider declares the type -> validation error at model load.
   Explicit namespace bypasses scan:
     type: aws.rds_instance

2. FACT RESOLUTION
   Primary provider defines canonical fact vocabulary.
   Supplementary providers contribute additional facts.
   Name collision -> primary wins.

3. PROBE RESOLUTION
   Providers run in pecking order. Primary first.
   Duplicate fact name in same probe run -> skip supplementary write.

4. DATA TYPE RESOLUTION
   Resolved against: provider data_types block, then mgtt stdlib.
     type: duration        # stdlib implicit
     type: mgtt.duration   # stdlib explicit — identical
```

### 6.4 Dependency Declarations

```yaml
depends:
  - on: postgres                     # while omitted -> provider default active state
  - on:    vault
    while: vault.state == starting   # only active during vault startup
  - on:    [primary_db, replica_db]
    while: primary_db.state == live | replica_db.state == live
```

When `while` is omitted, the provider's `default_active_state` is assumed.
Engineers only write `while` to express something different from the default.

### 6.5 The While Expression Grammar

Used in both `depends.while` and provider `states.when`.

```
<expr>       ::=  <condition>
               |  <expr> & <expr>
               |  <expr> | <expr>
               |  ( <expr> )

<condition>  ::=  <component>.<fact>  <operator> <value>
               |  <component>.<fact>  in [ <value>, ... ]
               |  <component>.state   == <state_name>
               |  <component>.state   != <state_name>
               |  <component>.state   in [ <state_name>, ... ]

<operator>   ::=  ==  !=  <  >  <=  >=

<value>      ::=  integer | float | bool | quoted string
               |  duration literal   5s | 1m | 2h
               |  bytes literal      10mb | 1gb
                  (unit valid only when fact.type declares it)
```

### 6.6 Model Validation

`mgtt model validate` checks structural, logical, and unit correctness. It
reports undefined states, undefined facts, tautologies, contradictions, and
unit mismatches against declared types. Errors include correction suggestions
(e.g. "did you mean 'live'?").

---

## 7. Fact Store Format

```yaml
# system.state.yaml

meta:
  model:    storefront
  version:  1.0
  incident: inc-20240205-0814-001
  started:  2024-02-05T08:14:00Z

facts:
  _system:
    - key:   state
      value: switching
      at:    2024-02-05T08:15:00Z

  api:
    - key:       endpoints
      value:     0
      collector: kubernetes
      at:        2024-02-05T08:15:12Z

    - key:       ready_replicas
      value:     0
      collector: kubernetes
      at:        2024-02-05T08:15:18Z

    - key:       startup_error
      value:     "missing module: ./config/feature-flags"
      collector: manual
      at:        2024-02-05T08:22:00Z
      note:      seen in kubectl logs --previous

  rds:
    - key:       available
      value:     true
      collector: aws
      at:        2024-02-05T08:15:31Z
```

### 7.1 Fact Store Rules

- Append only. Never edited or deleted.
- Each fact carries collector and observation timestamp.
- Facts reference the model version they were collected against.
- Each incident gets its own state file.
- `_system` is reserved for `mgtt` internal facts.
- Multiple collectors may contribute facts for the same component.

### 7.2 State as a Fact

Component states are derived continuously from collected facts using provider
state definitions. The current system-level state is stored as:

```yaml
facts:
  _system:
    - key:   state
      value: switching
      at:    2024-02-05T08:15:00Z
```

Derived automatically. Never set manually.

### 7.3 Fact Freshness

```
?   unchecked     no fact exists yet
~   stale         older than provider-declared TTL
ok  healthy       fresh, all conditions hold
x   unhealthy     fresh, one or more conditions violated
```

Stale facts are not used for elimination. `mgtt` suggests re-probing before
relying on them.

### 7.4 Incident Handoff

The state file is the complete incident record. To hand off mid-incident:

```bash
$ mgtt incident load inc-20240205-0814-001.state.yaml

incident loaded  8m elapsed, 5 facts, 2 paths eliminated
run 'mgtt plan' to continue
```

If the model version in the state file does not match the local model,
`mgtt` warns before proceeding.

---

## 8. Provider Format

Providers are external, Helm-plugin style. Each provider lives in its own git
repository and is installed into a local provider directory. The mgtt core
repository contains no provider-specific content.

A provider directory has two parts:

1. **`manifest.yaml`** — the vocabulary: types, facts, states, failure modes,
   healthy conditions. This is what the constraint engine reads. It never
   touches the real system.

2. **A provider binary** — the behavior: how to actually probe components,
   authenticate, parse responses. Runs at probe time. Speaks a simple protocol
   (args in, JSON out) and can be written in any language.

```
mgtt-provider-kubernetes/            (separate git repository)
├── manifest.yaml                    vocabulary for the engine
├── types/*.yaml                     one file per component type
├── hooks/
│   └── install.sh                   compiles or downloads the binary
├── go.mod, *.go                     source code
└── bin/
    └── mgtt-provider-kubernetes     the compiled provider binary
```

### 8.1 Top-Level Structure — manifest.yaml

```yaml
meta:
  name:        kubernetes
  version:     3.0.0
  description: Kubernetes workload and networking components
  requires:
    mgtt: ">=0.2.0"

runtime:
  needs: [kubectl]
  network_mode: host
  # entrypoint: optional; convention-default resolves to bin/mgtt-provider-kubernetes

install:
  source:
    build: hooks/install.sh
    clean: hooks/uninstall.sh
  image:
    repository: ghcr.io/mgt-tool/mgtt-provider-kubernetes

data_types:
  <name>:
    base:    <mgtt_stdlib_type>
    unit:    <suffixes> | ~
    range:   <range> | ~
    default: <value>

types:
  <name>:
    description:          <string>
    facts:                <fact_map>
    healthy:              <condition_list>
    states:               <state_map>
    default_active_state: <state_name>
    failure_modes:        <failure_mode_map>
```

A provider may split its `types` block across multiple files under `types/*.yaml`,
loaded and merged at provider load time. For the authoritative schema of the
three top-level blocks (`meta`, `runtime`, `install`) see
[manifest.yaml reference](reference/manifest.md).

### 8.2 The `meta` Block

| field       | required | description                                    |
|-------------|----------|------------------------------------------------|
| name        | yes      | unique, lowercase, hyphen-separated, `^[a-z][a-z0-9-]*$` |
| version     | yes      | semver                                         |
| description | yes      | one line                                       |
| tags        | no       | loose subject/topic labels (e.g. `workloads`, `storage`, `rbac`) — surfaced by `mgtt provider inspect` and mirrored by the community registry |
| requires    | yes      | `mgtt: "<version_constraint>"`                 |

### 8.2.1 The `runtime` Block

| field          | required | description                                              |
|----------------|----------|----------------------------------------------------------|
| needs          | no       | host-side capabilities (list or map form). See [Provider Capabilities](reference/image-capabilities.md). |
| backends       | no       | backend-service compatibility (list or map form).        |
| network_mode   | no       | `bridge` (default) or `host`.                            |
| entrypoint     | no       | path to provider binary; `$MGTT_PROVIDER_DIR` substituted. Convention-default resolves to `bin/mgtt-provider-<name>` for source installs and the image's baked-in `ENTRYPOINT` for image installs. |

### 8.2.2 The `install` Block

Declares which install methods the provider offers. At least one of `install.source` or `install.image` must be declared.

| field                   | required | description                                      |
|-------------------------|----------|--------------------------------------------------|
| source.build            | yes, if `install.source` declared | script run during `mgtt provider install` |
| source.clean            | no       | cleanup script run during `mgtt provider uninstall <name>` |
| image.repository        | no       | image repository; optional (defaults derive from the registry entry) |

Install scripts run with these environment variables:

| variable            | value                                             |
|---------------------|---------------------------------------------------|
| `MGTT_PROVIDER_DIR` | absolute path to the provider's install directory |
| `MGTT_PROVIDER_NAME`| provider name                                     |
| `MGTT_BIN`          | path to the mgtt binary                           |

### 8.2.3 The Provider Binary Protocol

The provider binary is a black box. mgtt calls it with args; it returns JSON
on stdout. The protocol has three commands:

**probe** — the primary operation:
```bash
mgtt-provider-kubernetes probe <component> <fact> \
  --namespace <ns> --type <type>

# stdout:
{"value": 0, "raw": "0"}
```

**validate** — check auth and connectivity:
```bash
mgtt-provider-kubernetes validate --namespace <ns>

# stdout:
{"ok": true, "auth": "kubectl context (eks-prod)", "access": "read-only"}
```

**describe** — self-declare capabilities (optional, supplements manifest.yaml):
```bash
mgtt-provider-kubernetes describe

# stdout: JSON matching the types block of manifest.yaml
```

Exit code 0 means success. Non-zero means error; stderr contains the message.

mgtt passes model variables as `--<key> <value>` args. The provider binary
receives the variables declared in `manifest.yaml`'s `variables` block.

### 8.3 The `data_types` Block

Provider-defined types built on stdlib primitives.

| field   | required | description                                        |
|---------|----------|----------------------------------------------------|
| base    | yes      | stdlib primitive: `mgtt.int`, `mgtt.duration`, etc |
| unit    | yes      | pipe-separated valid suffixes, or `~`              |
| range   | yes      | `<min>..<max>`, `<min>..`, `..<max>`, or `~`       |
| default | yes      | suggested value, must satisfy unit and range       |

### 8.4 The `facts` Block

| field   | required | description                                           |
|---------|----------|-------------------------------------------------------|
| type    | yes      | provider-defined or `mgtt.<primitive>`                |
| ttl     | yes      | duration after which fact is stale                    |
| probe   | no       | probe metadata (see 8.5)                              |
| default | no       | suggested threshold for use in healthy conditions     |

When the provider has a binary (`runtime.entrypoint` resolves to one, either
via the convention-default or an explicit override), the binary handles all
probing. The `probe` block in facts becomes metadata for the engine (cost,
access description) rather than an executable command. The binary receives
the component name and fact name as args and decides how to collect the data.

When the provider has no binary, the `probe.cmd` template is executed
directly by mgtt as a shell command.

### 8.5 Probe Metadata

| field   | required | description                                            |
|---------|----------|--------------------------------------------------------|
| cmd     | no       | command template — used when no provider binary exists |
| parse   | no       | how to interpret raw output (used with cmd fallback)   |
| unit    | no       | unit to attach to parsed numeric value                 |
| timeout | no       | max execution time, default 30s                        |
| cost    | no       | `low` \| `medium` \| `high`, default `low`             |
| access  | no       | human-readable description of required access          |

#### 8.5.1 Command Templates

Built-in variables always available:

| variable      | value                                     |
|---------------|-------------------------------------------|
| `{name}`      | component name as declared in model.yaml  |
| `{namespace}` | value of `namespace` model var if set     |
| `{provider}`  | name of the running provider              |

Provider-declared variables set in `model.meta.vars`:

```yaml
# manifest.yaml
variables:
  namespace:
    description: kubernetes namespace
    required:    false
    default:     default

# model.yaml
meta:
  vars:
    namespace: production
```

#### 8.5.2 Parse Modes

| parse value  | behaviour                                                  |
|--------------|------------------------------------------------------------|
| `int`        | trim whitespace, parse as integer                          |
| `float`      | trim whitespace, parse as float                            |
| `bool`       | `true/1/yes` -> true, `false/0/no` -> false                |
| `string`     | trim whitespace, use as-is                                 |
| `exit_code`  | exit 0 -> true, non-zero -> false                          |
| `json:<path>`| parse stdout as JSON, extract value at JSONPath            |
| `lines:N`    | count non-empty lines, return as int                       |
| `regex:<pat>`| apply regex, return first capture group as string          |

#### 8.5.3 Probe Error Handling

| outcome           | fact state | appended |
|-------------------|------------|----------|
| success, parsed   | ok or x    | yes      |
| success, unparsed | error      | no       |
| timeout           | ?          | no       |
| non-zero exit     | ?          | no       |

On failure, `mgtt` reports the error and offers a manual fact entry fallback.

### 8.6 The `healthy` Block

Conditions over same-component facts. Component prefix omitted.

```yaml
healthy:
  - ready_replicas == desired_replicas
  - endpoints > 0
  - restart_count < 5
```

All conditions ANDed. Component-level `healthy` in model.yaml replaces the
provider list entirely for that component.

### 8.7 The `states` Block

```yaml
states:
  starting:
    when:        ready_replicas < desired_replicas
    description: pods initialising
  live:
    when:        ready_replicas == desired_replicas
    description: all replicas ready
  degraded:
    when:        ready_replicas < desired_replicas & restart_count > 5
    description: crash-looping
  draining:
    when:        ready_replicas == 0
    description: fully offline

default_active_state: live
```

States are evaluated in declaration order. First match wins. If none match,
state is `unknown`. States use only same-component facts in `when` — no
cross-component references.

`mgtt` evaluates states continuously as facts arrive. Engineers never set
state manually.

### 8.8 The `failure_modes` Block

```yaml
failure_modes:
  degraded:
    can_cause: [upstream_failure, timeout, connection_refused]
  draining:
    can_cause: [upstream_failure, connection_refused]
```

Only non-healthy states need entries. `default_active_state` does not cause
downstream failures.

### 8.9 State Name Convention

Conventional names:

```
live, starting, degraded, draining, stopped, unknown
```

Deviation is acceptable for technology-specific vocabularies.

### 8.10 Type Name Uniqueness

Type names should be unique across the ecosystem. Where two providers clash,
use explicit namespace:

```yaml
type: kubernetes.deployment
```

---

## 9. Complete Provider Example

```yaml
# mgtt-provider-simplecache/manifest.yaml

meta:
  name:        simplecache
  version:     1.0.0
  description: SimpleCache in-memory cache server
  requires:
    mgtt: ">=1.0"

data_types:
  hit_ratio:
    base:    mgtt.ratio
    unit:    ~
    range:   0.0..1.0
    default: 0.9

  memory_used:
    base:    mgtt.bytes
    unit:    kb | mb | gb
    range:   0..
    default: 0mb

types:
  server:
    description: SimpleCache server instance

    facts:
      connected:
        type:  mgtt.bool
        ttl:   15s
        probe:
          cmd:    simplecache-cli ping {name}:{port}
          parse:  exit_code
          cost:   low
          access: network read

      hit_ratio:
        type:    hit_ratio
        ttl:     30s
        probe:
          cmd:    simplecache-cli stats {name}:{port} --field hit_ratio
          parse:  float
          cost:   low
          access: network read
        default: 0.9

      evictions_per_sec:
        type:  mgtt.float
        ttl:   30s
        probe:
          cmd:    simplecache-cli stats {name}:{port} --field evictions_per_sec
          parse:  float
          cost:   low
          access: network read

    healthy:
      - connected == true
      - hit_ratio > 0.5
      - evictions_per_sec < 100.0

    states:
      starting:
        when:        connected == false & evictions_per_sec == 0.0
        description: not yet accepting connections
      live:
        when:        connected == true & hit_ratio > 0.5
        description: connected and performing well
      degraded:
        when:        connected == true & hit_ratio <= 0.5
        description: high miss rate
      stopped:
        when:        connected == false
        description: not accepting connections

    default_active_state: live

    failure_modes:
      degraded:
        can_cause: [timeout, upstream_failure]
      stopped:
        can_cause: [upstream_failure, connection_refused]

variables:
  port:
    description: server port
    required:    false
    default:     11211
```

---

## 10. Provider Authoring Toolchain

### 10.1 CLI for Authors

```bash
mgtt provider init <name>        # scaffold provider directory from template
mgtt provider validate           # check manifest.yaml + binary against spec
mgtt provider test --readonly    # run probes in sandboxed read-only mode
mgtt provider publish            # submit to community registry

mgtt stdlib ls                   # list all stdlib types
mgtt stdlib inspect <type>       # full definition of a stdlib type
```

`mgtt provider init <name>` scaffolds the full provider directory:

```
<name>/
├── manifest.yaml                template with all fields documented
├── hooks/
│   └── install.sh               compiles the binary
├── go.mod                       Go module (for Go providers)
├── main.go                      skeleton implementing the protocol
└── README.md                    authoring guide
```

### 10.2 Writing a Provider

A provider consists of three things:

**1. The vocabulary (manifest.yaml):** fill in mgtt's schema with the
technology's specifics — `types`, `facts`, `states`, `healthy`, `failure_modes`.

**2. The binary:** implement the three-command protocol (§8.2.3): `probe`,
`validate`, `describe`. Any language. For Go providers, mgtt publishes a
convenience SDK (`mgtt/sdk`) with arg parsing and JSON serialization; it is
not required.

**3. The install script (`install.source.build`):** produces the binary. For
Go providers: `go build`. For Python: venv + pip install. For pre-compiled:
curl the right platform binary.

### 10.3 Provider Test Sandbox

`mgtt provider test --readonly`:

- calls the provider binary's `validate` command
- runs `probe` for each declared fact against a live system
- refuses any probe that writes, deletes, or modifies state
- requires explicit user permission before connecting
- logs all executions for audit

Probes that fail the read-only sandbox are rejected from the community registry.

### 10.4 Provider Validation Rules

```
meta:         name matches ^[a-z][a-z0-9-]*$, version semver, requires.mgtt valid
runtime:      needs entries resolve against capability vocabulary,
              network_mode is bridge|host|omitted
install:      at least one of install.source / install.image declared;
              install.source.build script exists if source declared
data_types:   base is mgtt stdlib, default satisfies unit and range
types:        at least one declared
facts:        types resolve, healthy refs declared facts
states:       when: refs declared facts only, default_active_state declared
failure_modes: keys are declared state names, default_active_state excluded
binary:       responds to 'validate', responds to 'probe' for each declared fact
```

### 10.5 Reference Provider

The reference provider is `mgtt-provider-kubernetes`
(`github.com/mgt-tool/mgtt-provider-kubernetes`). It uses the multi-file
`types/*.yaml` layout and demonstrates the full lifecycle: vocabulary
declaration, Go binary implementing the protocol, install hook, and test
coverage.

---

## 11. Authentication and Probe Execution

`mgtt` borrows Terraform's security model: the core knows nothing about
credentials. Providers declare their auth requirements. The environment
owns the credentials.

### 11.1 The Three Layers

```
mgtt core      ->   reasoning engine, never touches credentials
provider       ->   declares auth requirements, executes probes
environment    ->   owns credentials (env vars, files, instance roles)
```

### 11.2 Provider Write Posture + Capability Needs

`manifest.yaml` tells mgtt what a provider requires at probe time (inside `runtime:`) and what (if anything) it writes (top-level `read_only` / `writes_note`). Both are provider-level properties — they're declared the same way regardless of install method.

```yaml
# mgtt-provider-kubernetes/manifest.yaml
runtime:
  needs: [kubectl]          # host-side capabilities the provider wants
  network_mode: host        # docker-run network mode for image installs
read_only: true             # default; omit the field in pure-reader providers
```

```yaml
# mgtt-provider-terraform/manifest.yaml
runtime:
  needs: [terraform, aws]
  network_mode: host
read_only: false
writes_note: |
  The `drifted` fact runs `terraform plan` which refreshes state —
  a write to the state backend. Other facts are pure reads. Use a
  credential scoped so the provider literally cannot write, and omit
  the `drifted` fact, for hard read-only.
```

Credential-chain details (which env vars, which config-file paths the provider actually reads) live in each provider's README, where they can be narrative and accurate — not in a structured field that tools must pretend to parse.

See [Provider Capabilities](./reference/image-capabilities.md) for the `runtime.needs:` vocabulary, the operator-override file, and the `MGTT_IMAGE_CAPS_DENY` opt-out; see [Using Providers](./concepts/using-providers.md) for how mgtt composes these declarations into the `docker run` line for image-installed providers.

### 11.3 Probe Execution Modes

All modes feed the same engine — it receives a typed fact value either way.
The constraint engine and CLI never know which mode produced a fact.

**Provider binary (primary)** — mgtt calls the provider's binary via the
protocol defined in §8.2.3. The binary handles authentication, connection,
parsing, and type conversion internally.

**Shell fallback** — for providers without a binary, mgtt executes the
`probe.cmd` template from manifest.yaml directly in the local shell
environment.

**Fixture mode** — `$MGTT_FIXTURES` points to a YAML file with canned probe
outputs. No credentials, no binary, no shell. The parse pipeline still runs.

**Simulation** — fact values injected directly from scenario YAML. No probes
run at all. Tests the engine's reasoning, not the system.

```
provider binary     ->   production probing, proper types, auth handled
shell fallback      ->   prototyping, simple providers, no binary needed
fixture mode        ->   deterministic testing, golden files
simulation          ->   design-time model validation, CI
```

### 11.4 What `mgtt` Never Does

- Stores, manages, or rotates credentials
- Requests credentials from the engineer
- Transmits credentials over the network
- Executes write operations without `cost: high` flagging and confirmation

---

## 12. State Machine

### 12.1 Derivation

`mgtt` generates the state machine from:

```
provider state definitions   ->   per-component observable states
dependency graph             ->   structural composition
while conditions             ->   activation rules
```

Never authored. Never persisted. Never manually advanced.

Component states update automatically as facts arrive. When a new fact
causes a component's state to change, the constraint engine reruns
immediately.

### 12.2 Manual Override

When derived transitions need adjustment for a specific system:

```yaml
states:
  override:
    switching:
      relaxed:
        nginx:
          - upstream_count >= 0
```

Overrides are additive. The base machine is still derived first.

---

## 13. MCP Service

`mgtt` exposes its constraint engine as an MCP service, callable by LLMs
and AI agents. CLI and MCP are equal consumers.

### 13.1 Tools

```
mgtt://tools/plan          run constraint engine, return failure path tree
mgtt://tools/probe         run a probe, append fact, return updated tree
mgtt://tools/fact/add      add a manual fact, return updated tree
mgtt://tools/ls/components list components and current status
mgtt://tools/ls/facts      list facts for a component
```

### 13.2 Tool Schemas

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

### 13.3 Autonomy Modes

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

## 14. CLI

### 14.1 Incident

```bash
mgtt incident start                   # start incident, auto-generate id
mgtt incident start --id PD-892341    # correlate with external alert id
mgtt incident end                     # close, retain state file
mgtt incident ls                      # list past incidents
mgtt incident load <file>             # load state file for handoff
mgtt incident summary                 # current status, findings, duration
```

### 14.2 Planning

```bash
mgtt plan                             # start from outermost component
mgtt plan --component api             # start from known-bad component
```

`mgtt plan` is interactive. After showing the path tree and suggested probe
it prompts for confirmation. After confirmation it runs the probe, appends
the fact, and presents the updated plan.

### 14.3 Probing

```bash
mgtt probe <component>                # run all probes for component
mgtt probe <component> <fact>         # run one specific probe
mgtt probe skip <component> <fact>    # skip with optional reason
```

### 14.4 Facts

```bash
mgtt fact add <component> <key> <value>
mgtt fact add <component> <key> <value> --at <ISO8601>
mgtt fact add <component> <key> <value> --collector <provider>
mgtt fact add <component> <key> <value> --note "<text>"

mgtt ls facts                         # all facts, time sorted
mgtt ls facts <component>             # facts for one component
mgtt ls facts --unchecked             # only uncollected facts
mgtt ls facts --stale                 # only stale facts
```

### 14.5 Status

```bash
mgtt status                           # overall health summary
mgtt ls                               # components and current status
mgtt ls components                    # same
mgtt state                            # current state + derived machine (read-only)
```

### 14.6 Simulation

```bash
mgtt simulate --scenario <file>       # run one scenario
mgtt simulate --all                   # run all scenarios in scenarios/
```

### 14.7 Model

```bash
mgtt init                             # scaffold blank system.model.yaml
mgtt model validate                   # validate with correction suggestions
```

### 14.8 Providers

```bash
mgtt provider ls                      # installed providers
mgtt provider install <name>          # install
mgtt provider inspect <name>          # types, auth, access
mgtt provider inspect <name> <type>   # facts, probes, states, failure modes
mgtt provider init <name>             # scaffold new provider
mgtt provider validate                # check against spec
mgtt provider test --readonly         # sandboxed probe test
mgtt provider publish                 # submit to registry

mgtt stdlib ls                        # list stdlib types
mgtt stdlib inspect <type>            # full stdlib type definition
```

---

## 15. Simulation

`mgtt` provides value at design time. Writing `system.model.yaml` forces
explicit decisions about components, dependencies, and failure modes.
Simulation validates that the constraint engine reasons correctly over those
decisions.

Simulation tests the model's reasoning, not the system's behaviour. A passing
simulation means: if a component reports these facts, the engine guides
correctly. It says nothing about whether the component will actually fail.

`mgtt model validate` covers static correctness of invariants (`while`,
`healthy`). Simulation covers traversal — given these facts, does the engine
find the right root cause in the right order?

### 15.1 Scenarios

Scenarios live alongside the model in version control. Each injects a set
of facts and asserts what the constraint engine should conclude.

```yaml
# scenarios/rds-unavailable.yaml

name:        rds unavailable
description: >
  tests constraint engine traversal only — not system behaviour.
  if rds reports these facts, the engine should find rds as root cause.

inject:
  rds:
    available:        false
    connection_count: 0
  api:
    ready_replicas:   0
    restart_count:    12
    desired_replicas: 3

expect:
  root_cause: rds
  path:       [nginx, api, rds]
  eliminated: [frontend]
```

### 15.2 Running Simulations

```bash
mgtt simulate --scenario scenarios/rds-unavailable.yaml
mgtt simulate --all
```

`mgtt simulate` injects the scenario's facts, runs the constraint engine,
and compares the computed root cause, path, and eliminated components
against the `expect` block. Each scenario passes or fails with diagnostics
pointing to the model fact or state that caused the mismatch.

### 15.3 Simulation in CI

```yaml
# .github/workflows/mgtt.yaml

- name: validate model
  run:  mgtt model validate

- name: run scenarios
  run:  mgtt simulate --all
```

No running system. No credentials. Pure constraint engine evaluation.

### 15.4 Simulation vs Reality

```
mgtt model validate    ->  invariant expressions syntactically valid
                            no contradictions or tautologies
                            static analysis, no facts needed

mgtt simulate          ->  traversal behaves as expected
                            given these facts, engine finds this root cause
                            tests the wiring, not the system

real incident          ->  everything else
                            novel failures, unpredicted combinations
```

---

## 16. Design Principles

- **Zero cognitive load at incident time.** The on-call engineer presses Y.
  All system knowledge lives in the model, authored calmly beforehand.
- **Simple until explicit.** Defaults cover 90% of cases. Namespacing and
  overrides exist for the other 10%.
- **Pecking order is the single resolution rule.** Type, facts, probes,
  data types all resolve the same way: first provider wins.
- **State is observed, not declared.** Component states derive from facts
  automatically. Engineers never set or advance state.
- **Stdlib is primitives only.** Higher-level types belong in providers.
- **Credentials belong to the environment.** `mgtt` never stores, manages,
  or transmits credentials. Same model as Terraform.
- **Providers are self-contained and external.** Each provider lives in its
  own git repository. In v1.0, providers depend only on the mgtt stdlib.
- **AI friendly, not AI dependent.** MCP makes `mgtt` callable by any LLM.
  The constraint engine reasons — the AI drives the loop.
- **Append only.** The fact store is a record, not a scratchpad.
- **Derive, don't persist.** State machine and failure path tree computed
  fresh. Only observations and current position stored.
- **Engine is pure.** The constraint engine has no I/O, no probe execution,
  no credential access, no filesystem operations. It takes a model, providers,
  and facts as input and returns a failure path tree. The same engine is
  callable from the CLI, simulation runner, and MCP service — only the source
  of facts differs.
- **Guided, not automated.** `mgtt` tells you what to check next and why.
  Human or AI decides whether to check it.
