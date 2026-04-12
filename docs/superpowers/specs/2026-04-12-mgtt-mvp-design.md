# MGTT v0 — MVP Design

Date: 2026-04-12
Status: approved, ready for plan

## 1. Context and scope

MGTT is specified in `specs.md` as a model-guided troubleshooting tool. Today the project is spec-only — no implementation. This document defines the v0 implementation scope: the minimum Go code, providers, and test harness needed to make the two canonical modus operandi from the project README work end to end.

**In scope for v0:**

- The troubleshooting modus operandi (`troubleshooting-scenario.md`) — interactive `mgtt incident start → plan → [Y/n] loop → fact add → incident end` against a fixture backend.
- The simulation modus operandi (`simulation-scenario.md`) — `mgtt simulate --scenario` and `mgtt simulate --all` with the four storefront scenarios.
- A kubernetes provider with `ingress` and `deployment` types.
- A minimal aws provider with only `rds_instance`.
- A CLI surface: minimum commands both scenarios invoke, plus read-only inspectors (`ls`, `status`, `provider ls/inspect`, `stdlib ls`).
- TDD throughout, with scenario-doc output as golden-file inputs.

**Out of scope for v0 (explicit deferrals):**

- MCP service (spec §13).
- AWS provider surface beyond `rds_instance`.
- `mgtt incident ls | load | summary`, `mgtt probe` direct invocation, `mgtt probe skip`, `mgtt provider init | validate | test | publish`.
- `duration` and `bytes` literals in `while`/`healthy` expressions.
- `ref in [...]` expressions.
- Probability / Bayesian layer.
- File-level locking for concurrent incident access (see §10).
- Full JSONPath (we use a dot-path subset).
- Additive `healthy_also` semantics.
- Cross-provider type references.
- Real cluster / real AWS integration tests (fixture backend only in CI).

## 2. Decisions

| # | Decision |
|---|---|
| Language | Go |
| Repo layout | Same repo as the spec docs, Go module rooted at repo root |
| Provider distribution | `go:embed` bundled, with `$MGTT_HOME/providers/<name>/` filesystem override |
| CLI scope | Minimum for scenarios + read-only inspectors (option b in Q4) |
| Probe execution | `Executor` interface, `exec` and `fixture` backends, switched via `$MGTT_FIXTURES` env var |
| Testing | TDD strict; scenario docs provide golden-file transcripts |
| Interactive loop | `bufio.Scanner` and plain `[Y/n]` prompts, non-TTY auto-accept |
| Expression parser | Hand-rolled recursive descent, C-style `&` > `|` precedence |
| Cost weights | `low=1, medium=3, high=10`, tiebreak by distance from entry point |
| Unresolved handling | Typed `UnresolvedError{Component, Fact, Reason}`, `errors.As` at call sites |
| Concurrency | No locking in v0; append-rename atomicity per write; documented race |
| Panic safety | Top-level `recover` in `main`, bug-report message, exit 3 |
| Sequencing | Approach A — simulation-first vertical slice (see §10) |

## 3. Architecture and package layout

Single Go binary, `cmd/mgtt/main.go` wires cobra commands to internal packages. Internal packages are organized around the spec's conceptual boundaries.

```
/root/docs/projects/mgtt/
├── README.md, specs.md, troubleshooting-scenario.md, simulation-scenario.md
├── go.mod
├── cmd/mgtt/                   single main.go — wires cobra to internal pkgs
├── internal/
│   ├── model/                  system.model.yaml parsing, validation, graph
│   ├── facts/                  append-only fact store, state.yaml I/O
│   ├── expr/                   while/when grammar lexer, parser, evaluator
│   ├── state/                  state derivation from facts + provider state defs
│   ├── engine/                 constraint propagation, path tree, info-gain ranking
│   ├── provider/               provider.yaml loader, type resolution, pecking order
│   ├── probe/                  Executor interface, exec + fixture backends, parse
│   ├── simulate/               scenario loader, fact injection, expectation diff
│   ├── incident/               incident lifecycle, file naming, current session
│   ├── cli/                    cobra commands, one file per subcommand
│   └── render/                 terminal output — the only package writing to stdout
├── providers/                  go:embed source
│   ├── kubernetes/provider.yaml
│   └── aws/provider.yaml
├── fixtures/                   probe fixture files shipped with repo
│   └── storefront-incident.yaml
├── scenarios/                  simulation scenarios for the example storefront
│   ├── rds-unavailable.yaml
│   ├── api-crash-loop.yaml
│   ├── frontend-degraded.yaml
│   └── all-healthy.yaml
├── examples/
│   └── storefront/
│       └── system.model.yaml
└── testdata/
    ├── golden/                 expected CLI output, one file per invocation
    ├── fixtures/               test-only fixtures
    ├── models/                 test-only malformed/valid model files
    └── scenarios/              test-only scenarios
```

**Dependency direction:**

```
cli → {incident, facts, engine, probe, simulate, render, model, provider}
simulate → {model, provider, engine, facts}
engine → {model, provider, facts, state, expr}
state → {expr, provider, facts}
probe → {provider, (exec|fixture)}
provider → {expr}
model → {expr, provider}
render → {engine, model, facts, state, incident, simulate, provider}
incident → {facts}
facts → {}
expr → {}
```

Graph is acyclic. `expr` and `facts` are true leaves (no MGTT-internal dependencies). `render` is a **terminal sink** — it imports most packages for their types but nothing imports render except `cli`. The directional isolation ("data flows into render, never out") is the real invariant.

**Boundary rules:**

- `render` is the *only* package that writes to stdout/stderr. Other packages return structured data. `render` imports other packages for their output types but never mutates them or calls back into them — it is a pure consumer.
- `engine` is pure — no I/O, no probe execution, no filesystem access. Same input → same output. Engine accesses provider type definitions via query methods on `provider.Registry` (see below), not by reaching into raw structs.
- `probe` owns the `Executor` interface, probe command substitution, and shell-metachar validation. Backends satisfy the interface; swapping backends changes nothing above. The `cli` layer passes model vars and component name as parameters — `probe` never imports `model`.
- `provider` owns pecking-order resolution and exposes query methods for the engine. `model` and `engine` ask it "what facts does component X have?" and never touch YAML directly.
- `cli` is thin glue — each subcommand is ~30–80 lines.

**Provider query methods for engine decoupling:**

Instead of `engine` reaching directly into `provider.Type.FailureModes[stateName]` etc., `provider.Registry` exposes focused query methods:

```go
func (r *Registry) FailureModesFor(componentType string, stateName string) ([]string, error)
func (r *Registry) DefaultActiveStateFor(componentType string) (string, error)
func (r *Registry) HealthyConditionsFor(componentType string) ([]expr.Node, error)
func (r *Registry) FactsFor(componentType string) (map[string]*FactSpec, error)
func (r *Registry) StatesFor(componentType string) ([]StateDef, error)
func (r *Registry) ProbeCostFor(componentType string, factName string) (string, error)
```

The engine states what it needs; the provider implements the lookup. This insulates the engine from provider struct evolution and keeps the coupling to a specced interface rather than internal field layout. Direct struct access is still fine for one-shot reads elsewhere (e.g. `cli/provider_inspect` rendering the full type definition).

**Runtime data flow (troubleshooting):**

```
cli.incident.Start  → incident.New → initial state.yaml in CWD → .mgtt-current pointer
cli.plan            → model.Load → provider.Resolve → facts.Load →
                      engine.Plan(model, providers, facts, entry) → PathTree →
                      render.Plan(tree) → prompt [Y/n] →
                      probe.Executor.Run(suggested) → Result →
                      facts.Append(component, fact from result) → loop
cli.fact_add        → facts.Append → (optional) render preview
cli.incident.End    → incident.End → seal state file → render summary
```

**Runtime data flow (simulation):**

```
cli.simulate → simulate.Load(file) → Scenario → simulate.Run(model, providers, scenario) →
                 facts.NewInMemory → scenario.Inject → engine.Plan → match Expect → Result →
                 render.SimulateResult
```

Simulation reuses `engine.Plan` verbatim; only the fact store is in-memory and the loop runs once.

## 4. Core data model

### 4.1 model package

```go
type Model struct {
    Meta       Meta
    Components map[string]*Component
    Order      []string                // declaration order
    graph      *depGraph                // derived
}

type Meta struct {
    Name      string
    Version   string
    Providers []string
    Vars      map[string]string
}

type Component struct {
    Name          string
    Type          string
    Providers     []string              // nil → inherit Meta.Providers
    Depends       []Dependency
    Healthy       []expr.Node           // overrides provider healthy if non-empty
    FailureModes  map[string][]string   // state → can_cause overrides
}

type Dependency struct {
    On    []string
    While expr.Node                     // nil → always active (see §5.2)
}
```

- `graph` built during `model.Load` by walking `Depends`, gives O(1) upstream/downstream lookups and enables in-degree-zero entry-point detection.
- `model.Validate(m, providers) → ValidationResult` is pure; see §9.

### 4.2 facts package

```go
type Store struct {
    Meta  StoreMeta
    Facts map[string][]Fact           // component → ordered history
    mu    sync.Mutex                  // process-local serial append
}

type StoreMeta struct {
    Model    string
    Version  string
    Incident string
    Started  time.Time
}

type Fact struct {
    Key       string
    Value     any                     // parsed (int|float|bool|string), not raw
    Collector string                  // provider name or "manual" or "simulate"
    At        time.Time
    Note      string
    Raw       string                  // original probe stdout, for audit
}
```

Rules:

- `Append(component, fact)` is the only mutator. No update, no delete.
- `Latest(component, key) → *Fact` returns the most recent fact; older facts remain, just shadowed.
- `Freshness(fact, provider) → {Healthy, Stale, Unchecked, Unhealthy}` compares `fact.At` against the provider-declared TTL; matches spec §7.3.
- `Load` and `Save` marshal to `system.state.yaml` following spec §7. `_system` component reserved.
- In-memory stores (`NewInMemory`) skip all disk I/O — used by simulation.

### 4.3 expr package

Grammar subset for v0:

```
expr     = or
or       = and ("|" and)*
and      = primary ("&" primary)*
primary  = "(" expr ")" | ref cmp value
ref      = ident ("." ident)?
cmp      = "==" | "!=" | "<" | ">" | "<=" | ">="
value    = int | float | bool | string
```

Deferred: `in [...]`, `duration` literals, `bytes` literals.

Precedence: `|` binds loosest, `&` tighter, comparison tightest (C style).

```go
type Node interface { Eval(Ctx) (bool, error) }

type AndNode struct{ L, R Node }
type OrNode  struct{ L, R Node }
type CmpNode struct {
    Component string     // empty means use Ctx.CurrentComponent
    Fact      string     // or "state"
    Op        CmpOp
    Value     Value
}

type Ctx struct {
    CurrentComponent string
    States           map[string]string   // derived component states
    Facts            *facts.Store
}

type UnresolvedError struct {
    Component string
    Fact      string
    Reason    string                     // "missing" | "stale" | "type mismatch"
}
func (e *UnresolvedError) Error() string { ... }
```

Eval outcomes:

- `(true, nil)` — satisfied
- `(false, nil)` — definitely not satisfied; path/state eliminated
- `(false, *UnresolvedError)` — can't tell; path/state skipped, reason recorded

Callers use `errors.As(err, &u)` to distinguish. Type mismatches in values (string vs int on the same fact) return a non-`UnresolvedError` error and fail the containing command loudly.

Parser is hand-rolled recursive descent, tokenizer on `strings.IndexFunc`, expected ~150 lines. No parser generator, no external grammar library.

### 4.4 state package

```go
type Derivation struct {
    ComponentStates map[string]string   // component → state name
    UnresolvedBy    map[string][]UnresolvedError
}

func Derive(m *model.Model, p *provider.Registry, f *facts.Store) *Derivation
```

Algorithm (per component):

```
provider = resolve(component.Type)
for stateName, when in provider.States (declaration order):
    ok, err = when.Eval(Ctx{CurrentComponent: c, Facts: f, States: partial})
    if errors.As(err, &u):
        record to UnresolvedBy; try next state
    if err != nil:
        bubble up — programming error
    if ok:
        ComponentStates[c] = stateName
        break
if no match:
    ComponentStates[c] = "unknown"
```

First match wins. Provider YAML must order states most-specific to least-specific. A missing fact only blocks states that reference it; other states still get evaluated.

### 4.5 Type system and stdlib

The type system is two-layered: stdlib primitives (always available) and provider-defined types (built on stdlib). Both share the same Go representation.

```go
type DataType struct {
    Name    string
    Base    string     // stdlib primitive: "int", "float", "bool", "string"
    Units   []string   // valid suffixes: ["ms","s","m","h","d"] for duration; nil for unitless
    Range   *Range     // min..max constraints; nil for unconstrained
    Default any        // suggested value (provider-defined types only)
}

type Range struct {
    Min *float64       // nil = unbounded below
    Max *float64       // nil = unbounded above
}
```

**Stdlib** is a hardcoded built-in provider named `mgtt`, always registered first in `provider.Registry` before any installed provider. Not a YAML file — a Go `var` with the 10 primitives from spec §4.1:

```go
var Stdlib = map[string]DataType{
    "int":        {Name: "int",        Base: "int",    Units: nil,                              Range: nil},
    "float":      {Name: "float",      Base: "float",  Units: nil,                              Range: nil},
    "bool":       {Name: "bool",       Base: "bool",   Units: nil,                              Range: nil},
    "string":     {Name: "string",     Base: "string", Units: nil,                              Range: nil},
    "duration":   {Name: "duration",   Base: "float",  Units: []string{"ms","s","m","h","d"},   Range: &Range{Min: ptr(0.0)}},
    "bytes":      {Name: "bytes",      Base: "int",    Units: []string{"b","kb","mb","gb","tb"}, Range: &Range{Min: ptr(0.0)}},
    "ratio":      {Name: "ratio",      Base: "float",  Units: nil,                              Range: &Range{Min: ptr(0.0), Max: ptr(1.0)}},
    "percentage": {Name: "percentage", Base: "float",  Units: nil,                              Range: &Range{Min: ptr(0.0), Max: ptr(100.0)}},
    "count":      {Name: "count",      Base: "int",    Units: nil,                              Range: &Range{Min: ptr(0.0)}},
    "timestamp":  {Name: "timestamp",  Base: "string", Units: []string{"ISO8601"},              Range: nil},
}
```

**Provider-defined types** reference stdlib via `base`:

```yaml
# provider.yaml data_types block
data_types:
  hit_ratio:
    base:    mgtt.ratio        # resolves to stdlib ratio → base "float", range 0.0..1.0
    unit:    ~
    range:   0.0..1.0
    default: 0.9
```

Resolved at `provider.Load` time. `base` must resolve to a stdlib primitive or another provider-defined type that eventually resolves to a stdlib primitive (one level of chaining in v0; deeper chaining deferred with cross-provider refs).

**Type validation at three points:**

1. **`provider.Load`** — `data_types.base` must resolve to a stdlib primitive. `default` must satisfy declared `unit` and `range`. Missing or invalid → error with suggestion.

2. **`model.Validate`** — expression literals checked against the type of the referenced fact. `5s` against a `mgtt.duration` fact → valid. `5s` against a `mgtt.int` fact → error: "literal '5s' has unit 's' but fact 'restart_count' is type mgtt.int (unitless)."

3. **`facts.Append`** — value type-checked against the fact's declared type at collection time. An int fact rejects a string value; a duration fact rejects a bare int without unit context from the probe's parse mode.

**Expression literal resolution** (deferred in v0 for duration/bytes, but the pipeline is defined):

```
expression literal "5s"
  → parser recognises numeric + unit suffix "s"
  → looks up which DataType declares "s" as a valid unit → finds mgtt.duration
  → parses "5" as base type (float), attaches unit "s"
  → at eval time, compared against the referenced fact's value (also a duration)
  → comparison uses canonical units (all durations normalised to ms internally)
```

The storefront scenarios use only `mgtt.int` and `mgtt.bool` — no unit-suffixed literals. But the `DataType` struct, `Stdlib` var, and the validation pipeline at points 1 and 3 are implemented in v0 because they're needed for `mgtt stdlib ls/inspect` and for `provider.Load` to validate fact type declarations.

**`mgtt stdlib ls` and `mgtt stdlib inspect`** read from the `Stdlib` var:

```
$ mgtt stdlib ls

  int         base: int    unit: ~              range: ~
  float       base: float  unit: ~              range: ~
  bool        base: bool   unit: ~              range: ~
  string      base: string unit: ~              range: ~
  duration    base: float  unit: ms|s|m|h|d     range: 0..
  bytes       base: int    unit: b|kb|mb|gb|tb  range: 0..
  ratio       base: float  unit: ~              range: 0.0..1.0
  percentage  base: float  unit: ~              range: 0.0..100.0
  count       base: int    unit: ~              range: 0..
  timestamp   base: string unit: ISO8601        range: ~
```

### 4.6 engine package

```go
type PathTree struct {
    Entry      string
    Paths      []Path
    Eliminated []Path
    Suggested  *Probe                   // nil → root cause identified or no more probes
    RootCause  string                   // set when |Paths|==1 and terminal
    States     *state.Derivation        // snapshot, for rendering
}

type Path struct {
    ID         string                   // "PATH A"
    Components []string
    Hypothesis string                   // "rds.state == stopped"
    Reason     string                   // why eliminated (if eliminated)
}

type Probe struct {
    Component  string
    Fact       string
    Eliminates []string                  // path IDs healthy result would kill
    Cost       string                    // low | medium | high
    Access     string
    Command    string                    // rendered, substituted
}

func Plan(m *model.Model, p *provider.Registry, f *facts.Store, entry string) *PathTree
```

`entry` empty string means "use default outermost component." Explicit values come from `mgtt plan --component NAME`. `engine.Plan` is the single public entry point; CLI `plan`, `simulate`, and a future MCP service all call it.

## 5. Expression evaluator and constraint engine algorithm

### 5.1 State derivation subtleties

1. **Ordering matters:** e.g. kubernetes `deployment` must declare `degraded` (with `restart_count > 5`) *before* `starting` (without), because `starting`'s condition is a superset of `degraded`'s. `mgtt provider install` validation lints this.
2. **Missing fact narrows, doesn't block:** a component with `ready_replicas=0` and no `restart_count` resolves to `starting`, not `unknown`, because `degraded` records an unresolved and the evaluator tries `starting` next.

### 5.2 Engine stages

Pure functions, independently testable.

**Stage 1 — Entry selection**
```
if caller passed explicit component: use it
else: entry = component with in-degree 0 (outermost)
      if multiple: first in declaration order
```

**Stage 2 — Path enumeration**

Walk dependency graph inward from `entry`, following active edges. An edge `A depends on B` is active under these rules:

- **`while` omitted:** always active. The edge is a permanent structural dependency — this is the common case and covers every storefront scenario. Critically, this means the engine walks *through* unhealthy components, not around them; otherwise we couldn't descend from nginx through a degraded api to rds in scenario 1.
- **`while` present:** active iff the condition evaluates to `true` under current derived states. Covers transient dependencies like `api → vault while vault.state == starting` — when vault is live, api's vault-edge simply doesn't apply and isn't walked.
- **`while` unresolved:** treated as active (conservative) and the unresolved reason is recorded, so the renderer can explain why a later step inspected a branch whose activation we couldn't confirm.

Every maximal inward walk from `entry` through active edges is a candidate failure path.

```
PATH A  nginx → api
PATH B  nginx → api → rds
PATH C  nginx → frontend
```

Purely structural; does not look at failure modes yet.

**Stage 3 — Elimination**

For each path, walk from entry outward. If any component is known-healthy (derived state matches provider's `default_active_state` with fresh-enough facts), eliminate the path with reason "component X healthy." Unresolved states keep the path in play. Partial eliminations coexist: PATH A confirmed does not eliminate PATH B (rds might still be underlying cause) nor PATH C.

**Stage 4 — Failure-mode filtering**

Within each surviving path, check whether upstream failure `can_cause` values match the observed downstream symptoms (using the spec §5.6 vocabulary). Paths where the chain breaks rank lower ("weak hypothesis") but are not eliminated.

**Stage 5 — Probe ranking (information gain)**

For each candidate fact (unresolved or absent on live paths):

```
score    = |paths eliminated if probe returns healthy| / cost_weight
cost_weight = {low: 1, medium: 3, high: 10}
tiebreak = distance from entry (smaller is better)
```

Stale facts are treated as "worth re-probing" with a small score bump. The argmax becomes `tree.Suggested`. If no fact is informative, `Suggested = nil`.

### 5.3 Terminal conditions

`tree.RootCause` is set when exactly one path remains and its deepest component is either:
- in a non-`default_active_state` with no further inward dependencies to probe, or
- in a state whose `failure_modes` entry directly matches the original entry-point symptoms.

### 5.4 Scenario correspondence

| Scenario | Expected engine output |
|---|---|
| `rds-unavailable` | `root_cause=rds, path=[nginx,api,rds], eliminated=[frontend]` |
| `api-crash-loop` | `root_cause=api, path=[nginx,api], eliminated=[rds,frontend]` |
| `frontend-degraded` | `root_cause=frontend, path=[nginx,frontend], eliminated=[api,rds]` |
| `all-healthy` | `root_cause=none, eliminated=[all]` |

Each becomes a direct unit test of `engine.Plan`, independent of CLI rendering.

## 6. Providers, probes, and the executor interface

### 6.1 Registry

```go
type Registry struct {
    providers map[string]*Provider
    order     []string
}

type Provider struct {
    Meta      ProviderMeta
    DataTypes map[string]DataType
    Types     map[string]*Type
    Variables map[string]Variable
    Auth      AuthSpec
}

type Type struct {
    Name                string
    Description         string
    Facts               map[string]*FactSpec
    Healthy             []expr.Node
    States              []StateDef                 // ordered
    DefaultActiveState  string
    FailureModes        map[string][]string
}

type FactSpec struct {
    Type  string                                    // "mgtt.int" | "duration" | "hit_ratio" | ...
    TTL   time.Duration
    Probe ProbeDef
}

func (r *Registry) ResolveType(componentProviders []string, typeName string) (*Type, string, error)
```

Pecking order: scan `componentProviders` in order; first provider declaring `typeName` wins. Explicit namespace (`aws.rds_instance`) bypasses scan; owner must be the named provider. No match returns an error with suggestions.

### 6.2 The kubernetes provider

`providers/kubernetes/provider.yaml`:

```yaml
meta:
  name: kubernetes
  version: 1.0.0
  description: Kubernetes workload and networking components
  requires: { mgtt: ">=1.0" }

variables:
  namespace:
    description: kubernetes namespace
    required: false
    default: default

auth:
  strategy: environment
  reads_from: [KUBECONFIG, ~/.kube/config, in-cluster service account]
  access:
    probes: kubectl read-only
    writes: none

types:
  ingress:
    facts:
      upstream_count:
        type: mgtt.int
        ttl: 30s
        probe:
          cmd: kubectl -n {namespace} get endpoints {name} -o json
          parse: json:.subsets[0].addresses|length
          cost: low
          access: kubectl read-only
    healthy: [upstream_count > 0]
    states:
      live:     { when: "upstream_count > 0",  description: "serving traffic" }
      draining: { when: "upstream_count == 0", description: "no upstreams" }
    default_active_state: live
    failure_modes:
      draining: { can_cause: [upstream_failure, 5xx_errors] }

  deployment:
    facts:
      ready_replicas:
        type: mgtt.int
        ttl: 30s
        probe:
          cmd: kubectl -n {namespace} get deploy {name} -o jsonpath={.status.readyReplicas}
          parse: int
          cost: low
          access: kubectl read-only
      desired_replicas:
        type: mgtt.int
        ttl: 30s
        probe:
          cmd: kubectl -n {namespace} get deploy {name} -o jsonpath={.spec.replicas}
          parse: int
          cost: low
      restart_count:
        type: mgtt.int
        ttl: 30s
        probe:
          cmd: kubectl -n {namespace} get pods -l app={name} -o jsonpath={.items[*].status.containerStatuses[0].restartCount}
          parse: regex:(\d+)
          cost: low
      endpoints:
        type: mgtt.int
        ttl: 30s
        probe:
          cmd: kubectl -n {namespace} get endpoints {name} -o jsonpath={.subsets[0].addresses[*].ip}
          parse: lines:1
          cost: low
    healthy:
      - ready_replicas == desired_replicas
      - endpoints > 0
      - restart_count < 5
    states:
      degraded: { when: "ready_replicas < desired_replicas & restart_count > 5", description: "crash-looping" }
      draining: { when: "desired_replicas == 0",            description: "scaled to zero" }
      starting: { when: "ready_replicas < desired_replicas", description: "pods initialising" }
      live:     { when: "ready_replicas == desired_replicas", description: "all replicas ready" }
    default_active_state: live
    failure_modes:
      degraded: { can_cause: [upstream_failure, timeout, connection_refused, 5xx_errors] }
      draining: { can_cause: [upstream_failure, connection_refused] }
      starting: { can_cause: [upstream_failure, timeout] }
```

Two ordering-critical points:

1. `degraded` is declared before `starting`. Without this, any scenario with `ready_replicas < desired_replicas` resolves to `starting` immediately and `degraded` is unreachable. This is the exact subtlety `simulation-scenario.md` exposes: a crash-looping deployment looks identical to an initialising one until you check `restart_count`.
2. `draining` uses `desired_replicas == 0` (scaled-to-zero), not `ready_replicas == 0`. The latter is a strict subset of `starting`'s condition and would be unreachable after reordering. `desired_replicas == 0` is both Kubernetes-correct and independently reachable.

The state-ordering linter (model validation pass 7, §9.2) would catch either bug. Fixing them upfront in the embedded YAML prevents a warning at install time.

### 6.3 The aws provider (v0 minimal)

`providers/aws/provider.yaml`:

```yaml
meta:
  name: aws
  version: 0.1.0
  description: AWS resources (v0 — rds_instance only)
  requires: { mgtt: ">=1.0" }

auth:
  strategy: environment
  reads_from: [AWS_PROFILE, AWS_ACCESS_KEY_ID+SECRET, ~/.aws/credentials, instance profile]
  access:
    probes: AWS API read-only
    writes: none

types:
  rds_instance:
    facts:
      available:
        type: mgtt.bool
        ttl: 60s
        probe:
          cmd: aws rds describe-db-instances --db-instance-identifier {name} --query 'DBInstances[0].DBInstanceStatus' --output text
          parse: regex:^available$
          cost: low
          access: AWS API read-only
      connection_count:
        type: mgtt.int
        ttl: 60s
        probe:
          cmd: aws cloudwatch get-metric-statistics --namespace AWS/RDS --metric-name DatabaseConnections --dimensions Name=DBInstanceIdentifier,Value={name} --start-time $(date -u -d '5 minutes ago' +%FT%TZ) --end-time $(date -u +%FT%TZ) --period 60 --statistics Maximum --query 'Datapoints[0].Maximum' --output text
          parse: float
          cost: low
    healthy:
      - available == true
      - connection_count < 500
    states:
      live:    { when: "available == true",  description: "accepting connections" }
      stopped: { when: "available == false", description: "not accepting connections" }
    default_active_state: live
    failure_modes:
      stopped: { can_cause: [upstream_failure, connection_refused, query_timeout] }
```

### 6.4 Executor interface

```go
type Executor interface {
    Run(ctx context.Context, cmd Command) (Result, error)
}

type Command struct {
    Raw       string        // post-substitution
    Parse     string        // parse mode
    Provider  string
    Component string
    Fact      string
    Timeout   time.Duration
}

type Result struct {
    Raw      string
    Parsed   any
    ExitCode int
    Error    error
}
```

Two backends:

- `probe/exec/Executor` — shells out via `os/exec`, honors environment-owned auth.
- `probe/fixture/Executor` — reads fixture YAML keyed by `(provider, component, fact)`.

Selection:

```go
func NewExecutor() Executor {
    if path := os.Getenv("MGTT_FIXTURES"); path != "" {
        return fixture.Load(path)
    }
    return exec.Default()
}
```

### 6.5 Fixture file format

```yaml
# fixtures/storefront-incident.yaml
kubernetes:
  nginx:
    upstream_count: { stdout: "0\n", exit: 0 }
  api:
    endpoints:        { stdout: "\n", exit: 0 }
    ready_replicas:   { stdout: "0\n", exit: 0 }
    desired_replicas: { stdout: "3\n", exit: 0 }
    restart_count:    { stdout: "47\n", exit: 0 }
  frontend:
    ready_replicas:   { stdout: "2\n", exit: 0 }
    desired_replicas: { stdout: "2\n", exit: 0 }
    endpoints:        { stdout: "10.0.1.2\n10.0.1.3\n", exit: 0 }
aws:
  rds:
    available:        { stdout: "available\n", exit: 0 }
    connection_count: { stdout: "498\n", exit: 0 }
```

Raw stdout per fact. The same `parse:` machinery the exec backend uses extracts the value. One parser, tested once, covers both backends.

### 6.6 Parse modes

All eight from spec §8.5.2 implemented in `probe/parse.go`:

| Mode | Behaviour |
|---|---|
| `int` | trim, strconv.Atoi |
| `float` | trim, strconv.ParseFloat |
| `bool` | `true/1/yes` → true; `false/0/no` → false |
| `string` | trim |
| `exit_code` | exit 0 → true; non-zero → false |
| `json:<path>` | parse JSON, dot-path extract, `.N` for arrays, `\|length` for array size |
| `lines:<N>` | count non-empty lines, return int |
| `regex:<pat>` | apply regex, first capture group as string (or whole match if no group) |

Dot-path JSON resolver only — no full JSONPath. Rejected: `$`, `[?]` filters, `..` recursive, slicing. Sufficient for the v0 probes.

### 6.7 Probe command substitution

**Ownership: `probe` package.** Substitution, metachar validation, and the rendered command all live in `probe`. The `cli` layer passes model vars and the component name as parameters — `probe` never imports `model`.

Call flow:

```
cli/plan.go:
    rendered := probe.Substitute(probeDef.Cmd, component, model.Meta.Vars, providerVars)
    result, err := executor.Run(ctx, probe.Command{Raw: rendered, Parse: probeDef.Parse, ...})
```

```go
// probe/substitute.go
func Substitute(template string, component string, modelVars map[string]string, providerVars map[string]Variable) string
```

Variables substituted at probe-run time (not at provider-load time) because `{name}` and `{namespace}` depend on the component being probed and the model's `meta.vars`. Naive `strings.Replacer` on `{name}`, `{namespace}`, provider-declared vars. After substitution, `ValidateCommand(rendered, template)` checks that the resulting command contains no shell metacharacters that weren't already in the template (defensive against `name: "api; rm -rf /"` in a model file). Crude first layer; spec §17.8 tracks the open question for richer sandboxing.

### 6.8 Provider install

```
mgtt provider install <name>
  1. locate source: $MGTT_HOME/providers/<name>/provider.yaml if present, else embedded
  2. parse + validate (provider.Load)
  3. copy to ~/.mgtt/providers/<name>/provider.yaml (unless override is already there)
  4. register
  5. render summary: "✓ kubernetes v1.0.0  auth: kubectl context  access: read-only"
```

`mgtt provider ls` reads `~/.mgtt/providers/`. `mgtt provider inspect <name>` pretty-prints types/facts/states. `mgtt provider inspect <name> <type>` narrows.

## 7. CLI, plan loop, and simulation runner

### 7.1 Command surface

```
mgtt init                              scaffold blank system.model.yaml
mgtt model validate                    validate model against installed providers

mgtt provider install <name>...        embedded → ~/.mgtt/providers/<name>/
mgtt provider ls                       list installed
mgtt provider inspect <name> [<type>]  pretty-print definition

mgtt incident start [--id ID]          creates state.yaml in CWD
mgtt incident end                      seals file, prints summary

mgtt plan [--component NAME]           interactive Y/n probe loop
mgtt fact add <c> <k> <v> [--note ...] manual fact entry

mgtt ls                                components + current status
mgtt ls components                     alias
mgtt ls facts [<component>] [--stale | --unchecked]
mgtt status                            one-line health summary

mgtt simulate --scenario <file>        run one scenario
mgtt simulate --all                    run every file in scenarios/

mgtt stdlib ls                         list stdlib types
mgtt stdlib inspect <type>             full definition
```

### 7.2 Interactive `mgtt plan` loop

```
loop:
    tree = engine.Plan(model, providers, facts, entry)
    render.Plan(w, tree)
    if tree.RootCause != "":
        render.RootCauseSummary(w, tree); break
    if tree.Suggested == nil:
        render.NoMoreProbes(w, tree); break
    prompt "run? [Y/n] "
    if answer == "n":
        render.AskFallback(w, tree.Suggested); continue
    result, err = executor.Run(ctx, tree.Suggested.Command)
    if err != nil:
        render.ProbeError(w, err)
        offer manual fallback → facts.Append(manual fact) → continue
    facts.Append(component, fact from result)
```

- **Prompt reader:** `bufio.Scanner` on stdin. Empty → `y`. Case-insensitive.
- **Non-TTY mode:** `term.IsTerminal(os.Stdin)` false → auto-accept every probe, log one line per auto-accept so the transcript stays complete. Enables `MGTT_FIXTURES=... mgtt plan < /dev/null` as a golden-file test entry.
- **--component** pre-loads entry; passed to `engine.Plan` as `entry`.
- **Dead end** (`Suggested=nil` and `RootCause=""`): render "unable to proceed; add facts manually or extend the model." No auto-repair.

### 7.3 The render package

```go
func Plan(w io.Writer, tree *engine.PathTree)
func ModelValidate(w io.Writer, result *model.ValidationResult)
func IncidentStart(w io.Writer, inc *incident.Incident)
func IncidentEnd(w io.Writer, inc *incident.Incident, facts *facts.Store)
func ProviderLs(w io.Writer, providers []provider.Summary)
func ProviderInspect(w io.Writer, p *provider.Provider, typ string)
func SimulateResult(w io.Writer, result *simulate.Result)
func SimulateAll(w io.Writer, results []simulate.Result)
func Status(w io.Writer, facts *facts.Store, states *state.Derivation)
func FactsList(w io.Writer, facts *facts.Store, component string, flags ListFlags)
```

Single place output format lives. All functions take an `io.Writer` so tests can pass `bytes.Buffer`. Uses `text/tabwriter` for aligned columns. No ANSI color by default (flag `--color` opt-in later; v0 none).

Deterministic mode:

```go
var Deterministic bool  // set by tests; replaces time.Now(), elapsed calcs, random IDs with fixed values
```

### 7.4 Simulation runner

```go
type Scenario struct {
    Name        string
    Description string
    Inject      map[string]map[string]any  // component → key → value
    Expect      Expectation
}

type Expectation struct {
    RootCause  string
    Path       []string
    Eliminated []string
}

type Result struct {
    Scenario *Scenario
    Actual   Expectation
    Pass     bool
    Tree     *engine.PathTree            // for failure diagnostics
}

func Run(m *model.Model, p *provider.Registry, sc *Scenario) *Result {
    store := facts.NewInMemory()
    for c, kvs := range sc.Inject {
        for k, v := range kvs {
            store.Append(c, facts.Fact{Key: k, Value: v, Collector: "simulate", At: time.Now()})
        }
    }
    tree := engine.Plan(m, p, store, "")
    actual := extractConclusion(tree)
    return &Result{Scenario: sc, Actual: actual, Pass: matches(sc.Expect, actual), Tree: tree}
}
```

Failure output mirrors `simulation-scenario.md`: injected facts, engine conclusion, diff from expectation, and (critically) the reason any state went unresolved — pulled from `tree.States.UnresolvedBy`. This produces the "frontend.state could not be resolved — restart_count missing" message.

### 7.5 Incident lifecycle

```go
type Incident struct {
    ID        string     // inc-YYYYMMDD-HHMM-NNN or --id
    Model     string
    Version   string
    Started   time.Time
    Ended     time.Time  // zero until End
    StateFile string
}
```

- `Start` generates `inc-YYYYMMDD-HHMM-NNN` (counter per-minute), writes initial state.yaml with `meta:` block, writes `./.mgtt-current` (single-line pointer to state file). Subsequent `plan`/`fact add`/`end` in the same CWD read that pointer.
- `End` writes `ended` timestamp, renders summary, clears `.mgtt-current`.
- `Start` when one already exists → refuses with current ID; `--id NEW` to override with a loud warning.
- `End` with no active incident → error.

### 7.6 CLI → package dependency table

```
cli/init            → model
cli/model_validate  → model + provider
cli/provider_*      → provider
cli/incident_*      → incident + facts
cli/plan            → incident + facts + engine + probe + render
cli/fact_add        → facts + incident
cli/ls              → facts + state + render
cli/status          → facts + state + render
cli/simulate        → model + provider + simulate + render
cli/stdlib          → provider (stdlib is a built-in) + render
```

No cross-command imports. Each command file ~30–80 lines.

## 8. Testing strategy

TDD strict. Test taxonomy:

```
unit                  per package, pure functions only
                      (model, expr, state, engine, simulate, provider, probe/parse, facts)

package integration   cross-package, no CLI/filesystem/exec
                      e.g. engine+state+model+provider against in-memory store

golden CLI            run the binary (or cobra command tree) against fixtures,
                      diff stdout against testdata/golden/*.txt
                      uses probe/fixture backend

scenario              simulate.Run over scenarios/*.yaml against storefront model
                      phase 4 acceptance gate
```

No real cluster/AWS tests. The `exec` backend is unit-tested via a stub `os/exec` wrapper returning canned output.

### 8.1 Phase targets

```
Phase 0   skeleton                     go vet, go test (empty), CI yaml
Phase 1   model parser + validate      unit: model; golden: mgtt model validate
Phase 2   providers + registry         unit: provider.Load, pecking order;
                                       golden: mgtt provider install/ls/inspect
Phase 3   engine core                  unit: expr, state.Derive, engine.Plan (one scenario)
Phase 4   simulation end-to-end        all four scenarios pass;
                                       golden: mgtt simulate --all
Phase 5   facts + incident             unit: facts append/read/freshness;
                                       golden: mgtt fact add, ls, incident start/end
Phase 6   probe exec                   unit: parse modes, fixture backend
Phase 7   interactive plan loop        golden: full troubleshooting transcript
Phase 8   polish                       golden: status, stdlib, provider inspect
```

### 8.2 Phase 7 golden test

```
MGTT_FIXTURES=testdata/fixtures/storefront-incident.yaml \
  mgtt incident start --id test-inc-001 < /dev/null > actual.txt
MGTT_FIXTURES=testdata/fixtures/storefront-incident.yaml \
  mgtt plan < /dev/null >> actual.txt
mgtt fact add api startup_error "missing module: ./config/feature-flags" \
   --note "kubectl logs --previous" >> actual.txt
mgtt incident end >> actual.txt
diff actual.txt testdata/golden/troubleshooting-scenario.txt
```

### 8.3 Golden file discipline

- **Deterministic output or no golden test.** Timestamps, IDs, elapsed durations, ANSI color all elided or disabled via `render.Deterministic = true`.
- **Update protocol:** `go test ./... -update` (custom flag in `TestMain`) regenerates golden files. Never hand-edit. Review diffs in PR.
- **One golden file per command invocation.** Tests named after commands (`TestPlan_storefront_incident.golden`).

### 8.4 Scenario-doc extractor

Phase 0 deliverable: a script (probably `tools/extract-golden/main.go`) that walks `troubleshooting-scenario.md` and `simulation-scenario.md`, extracts fenced blocks following a `$ mgtt ...` prefix, emits `testdata/golden/<slug>.txt`. The docs become load-bearing — drift between docs and tool fails CI. Update both or neither.

### 8.5 testdata layout

```
testdata/
├── golden/
│   ├── model_validate_storefront.txt
│   ├── provider_install_kubernetes.txt
│   ├── provider_inspect_kubernetes_deployment.txt
│   ├── simulate_all_storefront.txt
│   ├── simulate_rds_unavailable.txt
│   ├── simulate_frontend_degraded_failing.txt
│   ├── plan_storefront_incident.txt
│   ├── incident_end_summary.txt
│   └── ...
├── fixtures/
│   └── storefront-incident.yaml
├── models/
│   ├── storefront.valid.yaml
│   ├── storefront.missing-dep.yaml
│   ├── storefront.circular.yaml
│   └── ...
└── scenarios/
    └── (test-only, separate from user-facing scenarios/)
```

User-facing `scenarios/` and `fixtures/` (at repo root) ship with the binary and must validate. `testdata/` is test-only and never installed.

### 8.6 Coverage targets

No numeric target. Rule: every public function in `engine`, `expr`, `state`, `simulate`, `probe/parse`, `facts` has at least one unit test. Every `cli/<command>` has at least one golden test. CI fails if any package drops below its current test count (regression guard, not coverage percentage).

### 8.7 CI

```yaml
# .github/workflows/ci.yaml
- go vet ./...
- go test ./...                          # unit + golden, no network, no cluster
- go build ./cmd/mgtt
- ./mgtt model validate                  # against examples/storefront/system.model.yaml
- ./mgtt simulate --all                  # eats our own dog food
```

The last two lines match the CI recipe the spec tells *users* to adopt — the project's own CI is the same thing we're selling.

## 9. Error handling and edge cases

### 9.1 Error taxonomy

| Kind | Example | Response |
|---|---|---|
| User error | wrong dir, missing provider | Friendly message, exit 1 |
| Model/config error | missing dep, cyclic graph, bad expression | `model validate` surfaces; other commands refuse until fixed |
| Runtime error | probe timeout, kubectl not in PATH, parse failure | Render failure, offer manual fallback, continue loop |

### 9.2 Model validation passes

```
1. structural       unknown fields, required missing, malformed YAML
2. type resolution  every component.type resolves; "did you mean X?"
3. dep references   every depends.on names an existing component
4. cycles           DFS coloring; report the cycle
5. expressions      every while/healthy/state when: parses;
                    unknown fact refs: "state 'running' not defined — did you mean 'live'?"
6. tautologies      x == y | x != y → warning
   contradictions   x < 5 & x > 10 → warning
7. state ordering   if state A's condition is a superset of B's and A declared after B → warn
```

Accumulating; single-pass produces every problem. `render.ModelValidate` sorts errors before warnings, matches spec §6.6.

### 9.3 Expression eval outcomes

```
(true,  nil)                    satisfied
(false, nil)                    not satisfied — path/state eliminated
(false, *UnresolvedError)       unknown — path/state skipped, reason recorded
(false, non-Unresolved err)     programming error — bubble up, fail loudly
```

### 9.4 Probe failure handling (spec §8.5.3)

| Outcome | Fact state | Appended? | Action |
|---|---|---|---|
| success, parsed | ✓ or ✗ | yes | continue loop |
| success, unparsed | error | no | render err, offer manual fallback |
| timeout | ? | no | render err, offer manual fallback |
| non-zero exit | ? | no | render err + stderr tail, offer manual fallback |

Manual fallback is a one-line `mgtt fact add ...` copy-pasteable. Non-TTY mode logs failure and continues; next `engine.Plan` re-surfaces the suggestion.

### 9.5 Fact store edge cases

- **Concurrent access:** no locking in v0. Documented: "don't run two mgtt processes against the same incident — second writer wins." Each write is append-rename atomic, so no torn files; worst case is lost append. Revisit in v1.
- **Model version drift:** state file loaded; warning banner before every command until `incident end`. Matches spec §7.4.
- **Corrupt state file:** parse fails with line/column; hint "restore from .state.yaml~" (append-rename leaves a sibling).

### 9.6 Graph edge cases

- **Cycles** → error at `model.Load` (DFS coloring). Names the cycle.
- **Disconnected subgraphs** → warning per isolated component.
- **Multiple roots** (several in-degree-zero) → entry defaults to first in declaration order with a warning; `--component` overrides.

### 9.7 CLI edge cases

- `mgtt plan` with no active incident → error, suggests `mgtt incident start`.
- `mgtt fact add` with no incident → same.
- `mgtt incident start` when one exists → refuses, prints current ID; `--id NEW` overrides with warning.
- SIGINT during `mgtt plan` → flush pending fact, "interrupted — state file preserved", exit 130. Append is the last step per iteration, so no partial writes.
- Non-existent provider during `model validate` → "provider X not installed; run `mgtt provider install X`."

### 9.8 Simulation edge cases

- Scenario references component not in model → parse error, file+line.
- `expect` path doesn't exist → scenario fails with diff, no crash.
- `simulate --all` with empty `scenarios/` → exit 0, "no scenarios found."

### 9.9 Panic safety

Core packages (`engine`, `expr`, `state`, `facts`, `provider`, `model`) must not panic on any YAML-sourced input. Parser errors return errors; evaluator errors return errors. `cmd/mgtt/main.go` has a top-level `recover` that prints a bug-report message and exits 3 — last-line defense, not a normal path.

## 9A. Architectural notes for future maintainability

Three items that are acceptable in v0 but should be revisited as the codebase grows:

1. **`state.Derive` receives the full `*facts.Store`** but only calls `Latest(component, key)`. If the fact store grows an interface (`FactLookup` with just `Latest`), `state` decouples from the store's I/O methods. Go's implicit interface satisfaction means this can be added later without changing callers.

2. **`model.Validate` orchestrates multiple concerns** — expression parsing, type resolution, graph analysis, tautology detection. If it grows past ~200 lines, split into focused validation passes (each in its own file within the `model` package, not separate packages). Keep the public API as `model.Validate()`.

3. **`render.Deterministic` is a package-level `var`**, not injected. A `RenderOptions{Deterministic bool, Now func() time.Time}` struct passed to each render function would be cleaner for testing and removes the global state. Cosmetic for v0.

These are not v0 blockers. They're flagged here so the plan doesn't have to invent them, and so future refactoring has a clear mandate.

## 10. Build sequencing (Approach A)

Simulation-first vertical slice. Each phase ends with a runnable binary that does strictly more than the previous.

### Phase 0 — Skeleton

- `go.mod`, directory layout, `cmd/mgtt/main.go` stub printing version.
- CI workflow (`go vet`, `go test`, `go build`).
- `tools/extract-golden/main.go` script, produces initial `testdata/golden/*.txt` from scenario docs.

**Acceptance:** `go build ./... && go test ./...` green; `./mgtt --version` prints; golden files materialized.

### Phase 1 — Model parser and validator

- `internal/model` types, YAML loader, dep graph construction.
- Model validation passes 1–4 (structural, type resolution, dep refs, cycles).
- Passes 5–7 deferred until `expr` exists in phase 3.
- `internal/render.ModelValidate`.
- `cli/init` and `cli/model_validate`.

**Acceptance:** unit tests for `model.Load`, `model.Validate`. Golden: `mgtt model validate` against `examples/storefront/system.model.yaml` matches the recorded output.

### Phase 2 — Provider loader and registry

- `internal/provider` types, provider.yaml loader, pecking-order resolution.
- `go:embed` of `providers/kubernetes/provider.yaml` and `providers/aws/provider.yaml` (YAML wrangled by hand to match the scenarios).
- `$MGTT_HOME` filesystem override.
- `cli/provider_install`, `cli/provider_ls`, `cli/provider_inspect`.

Note: provider.yaml fields that reference expressions (`healthy`, `states.when`) are loaded as raw strings in this phase; compiled to `expr.Node` in phase 3.

**Acceptance:** unit tests for pecking-order resolution, provider.yaml parsing. Golden: `mgtt provider install kubernetes aws`, `mgtt provider ls`, `mgtt provider inspect kubernetes deployment`.

### Phase 3 — Engine core (the big one)

- `internal/expr` lexer, parser, evaluator; `UnresolvedError` type.
- Provider expression strings compiled to `expr.Node` on load (now).
- `internal/state.Derive`.
- `internal/engine.Plan` stages 1–5.
- Model validation passes 5–7 (expressions, tautologies, ordering) added.

**Acceptance:** unit tests for every expression form, every eval outcome (including `UnresolvedError`), every engine stage, plus `engine.Plan` directly asserted against all four scenario expectations (using in-memory fact store).

### Phase 4 — Simulation end-to-end

- `internal/simulate.Load` (scenario YAML parser).
- `internal/simulate.Run`.
- `internal/render.SimulateResult`, `render.SimulateAll`.
- `cli/simulate` with `--scenario` and `--all`.
- Author the four storefront scenarios into `scenarios/`.

**Acceptance:** all four scenarios pass. Golden: `mgtt simulate --all`. Golden: `mgtt simulate --scenario scenarios/frontend-degraded.yaml` in its deliberately-broken state (matches the failing-then-fixed walkthrough in simulation-scenario.md).

**Simulation modus operandi done at this point.**

### Phase 5 — Facts and incident lifecycle

- `internal/facts` disk-backed store; `state.yaml` load/save; freshness.
- `internal/incident` lifecycle; `.mgtt-current` pointer.
- `internal/render.IncidentStart`, `IncidentEnd`, `FactsList`, `Status`.
- `cli/incident_start`, `cli/incident_end`, `cli/fact_add`, `cli/ls` (components+facts), `cli/status`.

**Acceptance:** unit tests for facts append/read/freshness. Golden: `mgtt incident start`, `mgtt fact add ...`, `mgtt ls facts`, `mgtt status`, `mgtt incident end`.

### Phase 6 — Probe execution

- `internal/probe` Executor interface.
- `probe/parse` — all eight parse modes.
- `probe/exec` — `os/exec` backend, unit-tested via stub.
- `probe/fixture` — YAML-backed backend.
- `probe.NewExecutor` env-var dispatch.

**Acceptance:** unit tests for every parse mode, fixture loader, exec stub. No end-to-end CLI test yet — that comes in phase 7.

### Phase 7 — Interactive plan loop

- `internal/render.Plan`, `RootCauseSummary`, `NoMoreProbes`, `ProbeError`, `AskFallback`.
- `cli/plan` — non-interactive/auto-accept when stdin not a TTY.
- Integration: plan loop → engine → render → prompt → executor → facts → engine.
- Author `fixtures/storefront-incident.yaml`.

**Acceptance:** phase 7 golden test (see §8.2). Output character-for-character matches `testdata/golden/troubleshooting-scenario.txt` (derived from troubleshooting-scenario.md by the extractor).

**Troubleshooting modus operandi done at this point.**

### Phase 8 — Polish

- `mgtt stdlib ls`, `mgtt stdlib inspect`.
- `mgtt provider inspect <name> <type>` refinements.
- `render.Status` and `render.FactsList` refinements as needed.
- README/user-facing install docs.

**Acceptance:** golden tests for inspector commands. All tests green. Binary builds cross-platform (Linux primary; Darwin secondary).

## 11. YAGNI list (things explicitly not built in v0)

- MCP service and tools (spec §13).
- `mgtt incident ls | load | summary`.
- `mgtt probe` direct invocation.
- `mgtt probe skip`.
- `mgtt provider init | validate | test | publish`.
- `duration` and `bytes` literals in expressions.
- `in [...]` operator.
- Full JSONPath (dot-path subset only).
- Bayesian/probability-ranked paths.
- File-level locking for concurrent access.
- Additive `healthy_also` semantics.
- Cross-provider type references.
- Real cluster / real AWS integration tests in CI.
- ANSI color output.
- Windows-specific flock equivalent.
- Structured command execution (argv, not strings) — defense is crude metacharacter rejection in v0.
- Model drift linter (`mgtt model check --against kubernetes`).
- Provider community registry.
- `--yes` flag for `mgtt plan` (non-TTY auto-accept covers it).

## 12. Success criteria for v0

- `mgtt simulate --all` against `examples/storefront/` passes all four scenarios.
- `mgtt simulate --scenario scenarios/frontend-degraded.yaml` (in its deliberately-broken state) fails with output matching the simulation-scenario.md walkthrough, including the "frontend.state could not be resolved — restart_count missing" reason.
- With `MGTT_FIXTURES=fixtures/storefront-incident.yaml`, `mgtt incident start && mgtt plan < /dev/null && mgtt fact add ... && mgtt incident end` produces output matching `troubleshooting-scenario.md` character-for-character.
- `mgtt model validate` on a model with a missing dep produces the spec §6.6 error with a suggestion.
- Single static binary distributable via `go build`. No runtime dependencies beyond `kubectl` and `aws` CLIs in the user's PATH (and only when real probing is enabled).
- CI runs `go test ./... && mgtt model validate && mgtt simulate --all` green, no network, no cluster.
