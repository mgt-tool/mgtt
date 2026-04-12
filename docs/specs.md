# MGTT — Model Guided Troubleshooting Tool
## Specification v1.0

---

## 1. What MGTT Is

MGTT is a tool that lets you encode a system model once, accumulate timestamped
observations into a fact store, and use constraint propagation over the model
and facts to guide — not replace — the troubleshooting process.

It is not a monitoring tool. It does not run continuously.
It is not an automation tool. It does not fix things.
It is not AI-dependent. AI is one possible consumer of the model, not a requirement.
It is not system-specific. The model language works for any distributed system.

The closest analogy is Terraform: separate desired state (model) from observed
state (facts), and reason over the diff. MGTT does this for understanding, not
provisioning.

**The core design principle for users:**

```
model author    writes system.model.yaml once, calmly, knows the system
on-call engineer  mgtt incident start
                  mgtt plan
                  [Y/n] y   ← repeat until done
                  mgtt incident end
```

Cognitive load belongs at authoring time, not incident time.

---

## 2. Core Concepts

### 2.1 Model
A YAML file describing the system: its components, their types, their
dependencies, and which providers own them. Written once by an engineer,
version-controlled alongside infrastructure code.

### 2.2 Fact Store
A YAML file of timestamped observations keyed by component. Append-only.
Written by MGTT during guided probing or manually via CLI. Scoped to an
incident session. Current system state is a fact like any other.

### 2.3 Provider
A plugin that owns one or more component types. Defines the vocabulary of
facts, data types, probes, states, failure modes, and healthy defaults for
its types. Built on the mgtt stdlib or from scratch. Community-maintained.

### 2.4 Stdlib
A built-in provider called `mgtt`, always available without import. Defines
primitive data types only — the lowest-level building blocks every provider
can use without declaring dependencies on each other.

### 2.5 Constraint Engine
MGTT's core. Takes four inputs — components, failure modes, propagation rules,
and current facts — and produces a ranked failure path tree. Starts from the
outermost component and works inward. Guides the engineer toward the next most
discriminating probe. Reruns after every new fact until one path remains.

### 2.6 Plan
The output of the constraint engine at any point in time: remaining failure
paths, eliminated paths, and the suggested next probe with reasoning.

### 2.7 State Machine
Derived automatically from the model and observed facts — never authored,
never persisted, never manually advanced. MGTT computes component states
from facts as they are collected. State transitions are observed, not declared.
Only the current position is stored as a fact.

### 2.8 MCP Service
MGTT exposes its constraint engine as an MCP (Model Context Protocol) service.
LLMs and AI agents can call MGTT tools directly, driving the same guided loop
a human would drive via CLI. CLI and MCP are equal consumers of the same engine.

---

## 3. The Three Artifacts

```
system.model.yaml       engineer writes once, version controlled
system.state.yaml       mgtt writes, append only, scoped per incident
providers/              community maintained, one per technology
```

The state machine and failure path tree are always derived on demand.
Neither is stored separately.

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

**Resolution chain:**

```
expression literal   "5s"
  → parser recognises unit suffix "s"
  → looks up which type declares "s" as a valid unit
  → finds mgtt.duration (base: float, unit: ms|s|m|h|d)
  → parses "5" as float, attaches unit "s"
  → value is valid iff fact.type is mgtt.duration or a provider type
    whose base is mgtt.duration
```

**Validation occurs at three points:**

```
provider load     data_types.base must resolve to a stdlib primitive
                  default must satisfy its own unit and range constraints

model validate    expression literals must match the type declared on
                  the referenced fact — 5s is valid against a duration
                  fact, error against an int fact

fact append       value type-checked against the fact's declared type
                  at collection time — int fact rejects a string value,
                  duration fact rejects a bare int without unit context
```

Unit-suffixed literals in expressions (5s, 10mb) are only valid when the
referenced fact's type declares that unit. `model validate` checks this
statically. If the types don't align, the error includes the expected type.

### 4.4 Cross-Provider References

Deferred to post-v1.0. In v1.0, providers reference only mgtt stdlib types.

---

## 5. The Constraint Engine

This is the core of MGTT. Everything else serves it.

### 5.1 Four Inputs

```
variables:          components
                    declared in model.yaml

domain:             possible failure modes per component
                    from provider failure_modes block

structural rules:   how failures propagate and when
                    from depends + while conditions in model.yaml

observations:       what we already know to be healthy or unhealthy
                    from the fact store — eliminates branches
```

**Entry point** — where traversal begins:

```
default:    outermost component in the dependency graph
            (the component nothing else depends on — ingress, load balancer)
            works for anyone, no system knowledge required

override:   --component flag — start from a known-bad component
            for engineers who already have a hunch

pre-loaded: mgtt fact add before mgtt plan
            when an alert has already fired with a known fact value
            tree is partially pruned before the first probe
```

Degraded inputs:

```
no facts         →   no elimination, all paths equally probable
no while conds   →   paths exist but no activation rules
no failure modes →   paths exist but no domain to reason over
no components    →   nothing
```

### 5.2 What the Engine Computes

Constraint propagation over the dependency graph produces a failure path tree:
all sequences of component failures that could explain observed unhealthy facts,
given what is already known.

```
entry point: nginx (outermost)
probe result: nginx.upstream_count = 0  ✗

PATH A  nginx ← api
          api.state == degraded | draining
          → probe: api.endpoints   cost: low

PATH B  nginx ← api ← rds
          rds.state == degraded | stopped
          → follows if PATH A confirmed

PATH C  nginx ← frontend
          frontend.state == degraded | draining
          → probe: frontend.endpoints   cost: low
```

### 5.3 Probe Ranking — Information Gain

```
maximise:   failure paths eliminated if probe returns healthy
minimise:   cost (time, access required, risk)
prefer:     facts closest to entry point first
```

### 5.4 The Guided Loop

```
entry point determined         →   outermost component or override
constraint propagation         →   computes reachable failure paths
fact store                     →   eliminates known-healthy branches
mgtt suggests next probe       →   highest information gain, lowest cost
probe runs                     →   new fact appended to store
state machine updates          →   component states derived from new facts
constraint propagation reruns  →   tree shrinks
repeat                         →   until one path remains → root cause
```

State is observed continuously as facts arrive. The engineer never
declares or advances state manually.

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

```
$ mgtt plan

  incident  inc-20240205-0814-001
  entry     nginx (outermost)
  state     switching  (observed from facts)
  paths     2 remaining · 1 eliminated

  failure paths:
  ────────────────────────────────────────────────────────────────────
  PATH A   nginx ← api
           api.state == degraded                    probability: high

  PATH C   nginx ← frontend
           frontend.state == degraded               probability: low

  eliminated:
  ────────────────────────────────────────────────────────────────────
  PATH B   nginx ← api ← rds    rds healthy ✓

  suggested next probe:
  ────────────────────────────────────────────────────────────────────
  → mgtt probe api ready_replicas
    eliminates: PATH A if healthy
    cost:       low
    access:     kubectl read-only

  fallback if probe unavailable:
  → mgtt fact add api ready_replicas <value>

  run? [Y/n]
```

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
meta.providers          →   default for all components
component.providers     →   override, applies to this component only
omitted at component    →   inherits meta.providers
```

### 6.2 Component Fields

| field         | required | description                                      |
|---------------|----------|--------------------------------------------------|
| providers     | no       | overrides meta.providers for this component      |
| type          | yes      | component type, resolved against provider list   |
| depends       | no       | list of dependency declarations                  |
| healthy       | no       | overrides or extends provider healthy defaults   |
| failure_modes | no       | overrides or extends provider failure modes      |

### 6.3 Providers — Pecking Order and Resolution Rules

```
1. TYPE RESOLUTION
   Scan providers list in order.
   First provider that declares the given type wins.
   That provider owns: schema, facts, data types, healthy, states, failure_modes.
   If no provider declares the type → validation error at model load.
   Explicit namespace bypasses scan:
     type: aws.rds_instance

2. FACT RESOLUTION
   Primary provider defines canonical fact vocabulary.
   Supplementary providers contribute additional facts.
   Name collision → primary wins.

3. PROBE RESOLUTION
   Providers run in pecking order. Primary first.
   Duplicate fact name in same probe run → skip supplementary write.

4. DATA TYPE RESOLUTION
   Resolved against: provider data_types block, then mgtt stdlib.
     type: duration        # stdlib implicit
     type: mgtt.duration   # stdlib explicit — identical
```

Summary:

```
providers list      →   ordered, index 0 = primary owner
primary provider    →   owns type, schema, facts, healthy, states
supplementary       →   contributes additional facts only
explicit namespace  →   provider.type bypasses order
conflict anywhere   →   primary wins
stdlib              →   always available, no import needed
```

### 6.4 Dependency Declarations

```yaml
depends:
  - on: postgres                     # while omitted → provider default active state
  - on:    vault
    while: vault.state == starting   # only active during vault startup
  - on:    [primary_db, replica_db]
    while: primary_db.state == live | replica_db.state == live
```

**While omitted:** provider's `default_active_state` is assumed. The engineer
only writes `while` to express something different from the default.

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

`mgtt model validate` checks structural, logical, and unit correctness.
On error, it suggests corrections — not just reports problems.

```
$ mgtt model validate

  ✓ nginx     2 dependencies valid
  ✓ api       1 dependency valid
  ✗ postgres  while: postgres.state == running
              error: state 'running' not defined in kubernetes/statefulset
              did you mean 'live'?
  ⚠ cache     while: cache.state == live | cache.state != live
              warning: tautology — condition always true

  1 error · 1 warning
```

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
- `_system` is reserved for MGTT internal facts.
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
✓   healthy       fresh, all conditions hold
✗   unhealthy     fresh, one or more conditions violated
```

Stale facts are not used for elimination. MGTT suggests re-probing before
relying on them.

### 7.4 Incident Handoff

The state file is the complete incident record. To hand off mid-incident:

```bash
# share the state file with a colleague
$ mgtt incident load inc-20240205-0814-001.state.yaml

✓ incident loaded  8m elapsed · 5 facts · 2 paths eliminated
  run 'mgtt plan' to continue
```

If the model version in the state file does not match the local model,
MGTT warns loudly before proceeding.

---

## 8. Provider Format

A provider is a directory containing at minimum a `provider.yaml` file.

### 8.1 Top-Level Structure

```yaml
meta:
  name:        kubernetes
  version:     1.2.0
  description: Kubernetes workload and networking components
  requires:
    mgtt: ">=1.0"

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

### 8.2 The `meta` Block

| field       | required | description                                    |
|-------------|----------|------------------------------------------------|
| name        | yes      | unique, lowercase, hyphen-separated            |
| version     | yes      | semver                                         |
| description | yes      | one line                                       |
| requires    | yes      | `mgtt: "<version_constraint>"`                 |

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
| probe   | yes      | probe definition (see 8.5)                            |
| default | no       | suggested threshold for use in healthy conditions     |

### 8.5 Probe Definition

| field   | required | description                                           |
|---------|----------|-------------------------------------------------------|
| cmd     | yes      | command template with `{variable}` substitution       |
| parse   | yes      | how to interpret raw output (see 8.5.1)               |
| unit    | no       | unit to attach to parsed numeric value                |
| timeout | no       | max execution time, default 30s                       |
| cost    | no       | `low` \| `medium` \| `high`, default `low`            |
| access  | no       | human-readable description of required access         |

#### 8.5.1 Command Templates

Built-in variables always available:

| variable      | value                                     |
|---------------|-------------------------------------------|
| `{name}`      | component name as declared in model.yaml  |
| `{namespace}` | value of `namespace` model var if set     |
| `{provider}`  | name of the running provider              |

Provider-declared variables set in `model.meta.vars`:

```yaml
# provider.yaml
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
| `bool`       | `true/1/yes` → true, `false/0/no` → false                 |
| `string`     | trim whitespace, use as-is                                 |
| `exit_code`  | exit 0 → true, non-zero → false                           |
| `json:<path>`| parse stdout as JSON, extract value at JSONPath            |
| `lines:N`    | count non-empty lines, return as int                       |
| `regex:<pat>`| apply regex, return first capture group as string          |

#### 8.5.3 Probe Error Handling

| outcome           | fact state | appended |
|-------------------|------------|----------|
| success, parsed   | ✓ or ✗     | yes      |
| success, unparsed | error      | no       |
| timeout           | ?          | no       |
| non-zero exit     | ?          | no       |

On failure, MGTT reports the error and offers a manual fact entry fallback.

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
state is `unknown`. States use only same-component facts in `when` —
no cross-component references.

MGTT evaluates states continuously as facts arrive. Engineers never set
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

Conventional names — not enforced:

```
live · starting · degraded · draining · stopped · unknown
```

Deviation is fine for technology-specific vocabularies.

### 8.10 Type Name Uniqueness

Type names should be unique across the ecosystem. Where two providers clash,
use explicit namespace:

```yaml
type: kubernetes.deployment
```

Provider authors check the community registry before naming new types.

---

## 9. Complete Provider Example

```yaml
# providers/simplecache/provider.yaml

meta:
  name:        simplecache
  version:     1.0.0
  description: SimpleCach in-memory cache server
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
    description: SimpleCach server instance

    facts:
      connected:
        type:  mgtt.bool
        ttl:   15s
        probe:
          cmd:    simplecach-cli ping {name}:{port}
          parse:  exit_code
          cost:   low
          access: network read

      hit_ratio:
        type:    hit_ratio
        ttl:     30s
        probe:
          cmd:    simplecach-cli stats {name}:{port} --field hit_ratio
          parse:  float
          cost:   low
          access: network read
        default: 0.9

      evictions_per_sec:
        type:  mgtt.float
        ttl:   30s
        probe:
          cmd:    simplecach-cli stats {name}:{port} --field evictions_per_sec
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

```bash
mgtt provider init <name>        # scaffold provider.yaml from template
mgtt provider validate           # check against spec contract
mgtt provider test --readonly    # run probes in sandboxed read-only mode
mgtt provider publish            # submit to community registry

mgtt stdlib ls                   # list all stdlib types
mgtt stdlib inspect <type>       # full definition of a stdlib type
```

### 10.1 Provider Test Sandbox

`mgtt provider test --readonly`:

- executes only probe commands declared in the provider
- refuses any probe that writes, deletes, or modifies state
- requires explicit user permission before connecting to any live system
- logs all executions for audit

Probes that fail the read-only sandbox are rejected from the community registry.

### 10.2 Provider Validation Rules

```
meta:         name lowercase+hyphen · version semver · requires.mgtt valid
data_types:   base is mgtt stdlib · default satisfies unit and range
types:        at least one declared
facts:        types resolve · healthy refs declared facts
states:       when: refs declared facts only · default_active_state declared
failure_modes: keys are declared state names · default_active_state excluded
probes:       cmd uses only declared variables · no write operations
              parse mode valid · cost is low|medium|high
```

### 10.3 Reference Runner

The kubernetes provider ships a reference runner — a shell script that
implements the runner delegation interface. Provider authors use it as a
starting point for their own runners.

```bash
# runner interface — two commands
mgtt probe next --format json    # get next probe to execute
mgtt fact add <c> <k> <v>        # report result back to mgtt
```

The reference runner wraps kubectl, handles kubeconfig context, and
calls these two commands. It is the proof that the interface works.

---

## 11. Authentication and Probe Execution

MGTT borrows Terraform's security model: the core knows nothing about
credentials. Providers declare their auth requirements. The environment
owns the credentials.

### 11.1 The Three Layers

```
mgtt core      →   reasoning engine, never touches credentials
provider       →   declares auth requirements, executes probes
environment    →   owns credentials (env vars, files, instance roles)
```

### 11.2 Provider Auth Declaration

```yaml
# providers/kubernetes/provider.yaml
auth:
  strategy:   environment
  reads_from:
    - KUBECONFIG
    - ~/.kube/config
    - in-cluster service account
  access:
    probes:  kubectl read-only
    writes:  none
  description: >
    uses the current kubectl context.
    configure with kubectl config use-context before mgtt.
```

```yaml
# providers/aws/provider.yaml
auth:
  strategy:   environment
  reads_from:
    - AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY
    - AWS_PROFILE
    - ~/.aws/credentials
    - EC2 instance profile / ECS task role / IRSA
  access:
    probes:  AWS API read-only
    writes:  none
  description: >
    uses the standard AWS credential chain.
    configure as you would for the AWS CLI.
```

### 11.3 Probe Execution Modes

Both modes implement the same contract: given a probe command and a parse
mode, execute the command and return the raw output. The constraint engine
and CLI never know which mode is active — they receive a parsed fact either
way. This single interface is what allows simulation (injected facts, no
probes), fixture-based testing (canned output, no credentials), direct
execution, and runner delegation to all feed the same engine without
branching.

**Direct execution (default)** — MGTT runs probe commands in the local
environment. Works wherever the engineer already has credentials configured.

**Runner delegation** — MGTT delegates to a runner process that runs where
the systems are. The runner executes probes and reports facts back via
`mgtt fact add`. Used for CI/CD pipelines, restricted environments, and
AI autonomous mode in production.

```
direct execution    →   local incident response, engineer's laptop
runner delegation   →   CI/CD, bastion hosts, AI autonomous, multi-team
```

### 11.4 What MGTT Never Does

- Stores, manages, or rotates credentials
- Requests credentials from the engineer
- Transmits credentials over the network
- Executes write operations without `cost: high` flagging and confirmation

---

## 12. State Machine

### 12.1 Derivation

MGTT generates the state machine from:

```
provider state definitions   →   per-component observable states
dependency graph             →   structural composition
while conditions             →   activation rules
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

Overrides are additive. Base machine still derived first.

---

## 13. MCP Service

MGTT exposes its constraint engine as an MCP service, callable by LLMs
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

### 13.4 A Full AI Loop

```
AI calls:     plan {}
MGTT returns: entry nginx · 3 paths · suggested: nginx.upstream_count · low

AI:           assist + low + read-only → run
AI calls:     probe { component: "nginx", fact: "upstream_count" }
MGTT returns: upstream_count=0 · following inward · new suggestion

AI:           assist + low + read-only → run
AI calls:     probe { component: "api", fact: "endpoints" }
MGTT returns: endpoints=0 · PATH A confirmed · PATH C eliminated

AI:           assist + low + read-only → run
AI calls:     probe { component: "api", fact: "restart_count" }
MGTT returns: restart_count=47 · api.state: degraded · root cause identified

AI reports:   "api is crash-looping (47 restarts, 0/3 replicas).
               rds is healthy and eliminated.
               Check api logs for startup errors."
```

---

## 14. CLI

The CLI follows the ergonomics of docker. Discoverable, scannable,
`--help` always complete. Designed for zero cognitive load during incidents.

### 14.1 Incident (primary workflow)

```bash
mgtt incident start                   # start incident, auto-generate id
mgtt incident start --id PD-892341    # correlate with external alert id
mgtt incident end                     # close, retain state file
mgtt incident ls                      # list past incidents
mgtt incident load <file>             # load state file for handoff
mgtt incident summary                 # current status, findings, duration
```

### 14.2 Planning (the core loop)

```bash
mgtt plan                             # start from outermost component
mgtt plan --component api             # start from known-bad component
```

`mgtt plan` is interactive. After showing the path tree and suggested probe
it prompts for confirmation. After confirmation it runs the probe, appends
the fact, and presents the updated plan. The engineer presses Y repeatedly.

### 14.3 Probing

```bash
mgtt probe <component>                # run all probes for component
mgtt probe <component> <fact>         # run one specific probe
mgtt probe skip <component> <fact>    # skip with optional reason
```

Probes are normally invoked by `mgtt plan`. Direct invocation for manual
workflows or when the suggested probe isn't the right one.

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

### 14.6 Simulation (design-time)

```bash
mgtt simulate --scenario <file>       # run one scenario
mgtt simulate --all                   # run all scenarios in scenarios/
```

Simulation injects facts and asserts constraint engine conclusions.
No running system or credentials required.

### 14.7 Model

```bash
mgtt init                             # scaffold blank system.model.yaml
mgtt model validate                   # validate with correction suggestions
```

### 14.7 Providers (authoring toolchain)

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

## 15. Design-Time Workflow — Simulation

MGTT provides value before a system is deployed. Writing `system.model.yaml`
forces explicit decisions about components, dependencies, and failure modes.
Simulation validates that the constraint engine reasons correctly over those
decisions — before any real system exists.

**What simulation tests:**

Simulation tests the model's reasoning, not the system's behaviour. A passing
simulation means: if a component reports these facts, the engine will guide
you correctly. It says nothing about whether or how the component will
actually fail. This is the same scope as a unit test — it tests the thing
it tests, nothing more.

The invariants (`while`, `healthy` conditions) are already validated
statically by `mgtt model validate`. Simulation validates the traversal —
given these facts, does the engine find the right root cause in the right
order?

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

```yaml
# scenarios/api-crash-loop.yaml

name:        api crash-loop, rds healthy
description: >
  verifies rds is correctly eliminated when api is degraded
  but rds probes return healthy values.

inject:
  api:
    ready_replicas:   0
    restart_count:    24
    desired_replicas: 3
  rds:
    available:        true
    connection_count: 120

expect:
  root_cause: api
  path:       [nginx, api]
  eliminated: [rds, frontend]
```

### 15.2 Running Simulations

```bash
mgtt simulate --scenario scenarios/rds-unavailable.yaml
mgtt simulate --all                   # run all scenarios in scenarios/
```

Output:

```
$ mgtt simulate --scenario scenarios/rds-unavailable.yaml

  scenario  rds unavailable
  mode      simulation — no real system contacted

  injecting facts:
    rds.available         = false
    rds.connection_count  = 0
    api.ready_replicas    = 0
    api.restart_count     = 12
    api.desired_replicas  = 3

  constraint engine result:

  ✓ root_cause  rds        (expected: rds)
  ✓ path        nginx ← api ← rds  (expected)
  ✓ eliminated  frontend   (expected)

  scenario passed
```

```
$ mgtt simulate --all

  rds-unavailable       ✓ passed
  api-crash-loop        ✓ passed
  frontend-oom          ✗ failed
    expected root_cause: frontend
    got:     no root cause — frontend.state could not be resolved
    reason:  no fact declared that signals frontend oom state
             add restart_count or memory_used to frontend type facts

  2/3 scenarios passed
```

Failures mean the model is incomplete for that specific traversal — not
that the system is broken. Fix the model until the scenario passes.

### 15.3 Simulation in CI

```yaml
# .github/workflows/mgtt.yaml

- name: validate model
  run:  mgtt model validate

- name: run scenarios
  run:  mgtt simulate --all
```

No running system. No credentials. Pure constraint engine evaluation.
Catches model regressions before they become incident blind spots.

### 15.4 Simulation vs Reality

```
mgtt model validate    →  invariant expressions syntactically valid
                           no contradictions or tautologies
                           static analysis, no facts needed

mgtt simulate          →  traversal behaves as expected
                           given these facts, engine finds this root cause
                           tests the wiring, not the system

real incident          →  everything else
                           novel failures, unpredicted combinations,
                           things nobody modelled
                           simulation makes no claims here
```

Simulation is coverage of the model's reasoning, not coverage of the
system. A passing simulation is not a guarantee of detection.

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
  Prevents semantic collision across the ecosystem.
- **Credentials belong to the environment.** MGTT never stores, manages,
  or transmits credentials. Same model as Terraform.
- **Providers are self-contained.** In v1.0, providers depend only on
  the mgtt stdlib. No provider-to-provider dependencies.
- **AI friendly, not AI dependent.** MCP makes MGTT callable by any LLM.
  The constraint engine reasons — the AI drives the loop.
- **Append only.** The fact store is a record, not a scratchpad.
- **Derive, don't persist.** State machine and failure path tree computed
  fresh. Only observations and current position stored.
- **Engine is pure.** The constraint engine has no I/O, no probe execution,
  no credential access, no filesystem operations. It takes a model, providers,
  and facts as input and returns a failure path tree. This is what makes the
  same engine callable from the CLI, simulation runner, and MCP service
  without branching — only the source of facts differs.
- **Guided, not automated.** MGTT tells you what to check next and why.
  Human or AI decides whether to check it.

---

## 17. Open Questions

1. **Model drift** — no automated comparison of model to live system in v1.0.
   Engineers add missing components manually when dead-end detection fires.
   A `mgtt model check --against kubernetes` linter is the right post-v1.0
   addition — validates the model against live state without generating it.

2. **Incident corpus** — anonymized state files are a valuable community
   artifact. Contribution and storage mechanism unspecified.

3. **Constraint engine reference implementation** — algorithm specified
   (arc consistency, information gain ranking), not yet implemented.
   Required before providers can be validated against it.

4. **Probability model** — information gain ranking without probabilities
   is a known approximation. Bayesian layer deferred to post-v1.0.

5. **MCP service authentication** — how an AI agent is authorised to call
   MGTT tools over MCP is separate from provider authentication. Unspecified.

6. **Cross-provider type references** — deferred to post-v1.0. Requires
   provider dependency graph and version conflict resolution.

7. **Healthy override semantics** — component-level `healthy` replaces
   the provider list entirely. Additive `healthy_also` deferred to post-v1.0.

8. **Probe shell safety** — commands restricted to pipes in v1.0. Richer
   sandboxing (seccomp, containerisation) needed for untrusted providers.

---

*MGTT is open to the world. Providers, incident corpora, and tooling
contributions are welcome. The spec and core are the only things under
tight governance. Everything else belongs to the community.*