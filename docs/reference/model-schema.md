# Model Schema Reference

Complete reference for `system.model.yaml` — the file that describes your system.

## On this page

- [Minimal example](#minimal-example)
- [Full schema](#full-schema)
- [`meta` section](#meta-section) — system identity, providers, vars
- [`components` section](#components-section) — declaring what mgtt reasons about
    - [Dependencies](#dependencies)
    - [Health expressions](#health-expressions) — operators, fact references
- [Complete example](#complete-example)
- [Validation](#validation) — what `mgtt model validate` checks

---

## Minimal example

```yaml
meta:
  name: my-system
  version: "1.0"
  providers:
    - kubernetes

components:
  api:
    type: deployment
    depends:
      - on: db

  db:
    type: rds_instance
    providers:
      - aws
```

## Full schema

```yaml
meta:
  name: <string>            # required — system name
  version: <string>         # required — model version (semver)
  providers:                # optional — default providers for all components
    - <provider-name>
  vars:                     # optional — variables substituted into probe commands
    <key>: <value>
  strict_types: <bool>      # optional — reject the generic fallback at validate time
  scenarios: none           # optional — opt out of scenarios.yaml generation + drift check

components:
  <component-name>:
    type: <type-name>       # required — a type defined by a provider
    resource: <string>      # optional — upstream resource id the provider probes
                            #   (supports {key} placeholders from meta.vars)
    providers:              # optional — override meta.providers for this component
      - <provider-name>
    depends:                # optional — components this one depends on
      - on: <component-name>
    healthy:                # optional — additional health conditions
      - <expression>
    vars:                   # optional — per-component overrides of meta.vars
      <key>: <value>
```

---

## `meta` section

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | System name. Used in output and state file naming. |
| `version` | yes | Model version. Quoted string — `"1.0"`, not `1.0`. |
| `providers` | no | List of provider names. Default providers for all components. When omitted or a type isn't matched, mgtt falls back to a built-in `generic.component` that prompts the operator interactively. Install typed providers with `mgtt provider install <name>`. |
| `vars` | no | Key-value pairs substituted into probe commands as `{key}`. Common: `namespace`, `region`, `cluster`. |
| `strict_types` | no | `true` rejects the generic fallback: any component whose type has no typed provider becomes a validation error. Default `false` emits one `INFO` line per component that falls back. |
| `scenarios` | no | Set to `none` to opt the model out of `scenarios.yaml` generation and drift detection. Use for empty placeholder models or works-in-progress where the sidecar would be meaningless. |

## `components` section

Each key under `components` is the component name. Names must be unique within the model.

| Field | Required | Description |
|-------|----------|-------------|
| `type` | yes | A type defined by one of the listed providers. See [Type Catalog](type-catalog.md) for available types. |
| `resource` | no | Upstream resource identifier. When set, the provider looks up `<resource>` instead of the component key at probe time. Lets you keep readable component keys (e.g. `rds:`) while probing the real backing resource (e.g. an RDS DB instance id, a kubectl-named Deployment, a Docker container name — whatever the owning provider expects). Supports `{key}` placeholders that expand against `meta.vars` at load time — a model shipped across environments can use `resource: my-database-{env}`. Unresolved placeholders are a load-time error. |
| `providers` | no | Override `meta.providers` for this component. Use when one component belongs to a provider different from the model's default set (e.g., a single RDS instance in a model whose defaults are Kubernetes). |
| `depends` | no | List of dependency entries. See [Dependencies](#dependencies) below. |
| `healthy` | no | Additional health conditions beyond the provider's defaults. See [Health expressions](#health-expressions) below. |
| `vars` | no | Per-component overrides of `meta.vars`. Merged on top of `meta.vars` at probe time — component keys win on collision, missing keys fall through to the model-wide default. Use when a single component lives in a different namespace, region, or cluster from the rest. Example: an ExternalSecrets operator in namespace `external-secrets` in a model whose other components live in `default`. |

### Readable component keys vs. provider resource identifiers

Real infrastructure identifiers are often noisy (`E3AB12CD34EF56`, `my-app-prod-media-a12d4c`, `/config/prod/env_php`). Use `resource:` to keep the model's dependency graph readable while probes hit the real resources:

```yaml
components:
  rds:
    type: rds_instance
    resource: my-database-name
  cdn:
    type: cloudfront_distribution
    resource: E3AB12CD34EF56
```

`meta.vars` substitution lets a single model ship across environments:

```yaml
meta:
  vars:
    env: stage
components:
  rds:
    type: rds_instance
    resource: my-database-{env}   # expands to my-database-stage
```

Unresolved `{key}` placeholders are a load-time error — better to fail here than to produce a literal `{env}` string in a kubectl call at 3am.

### Dependencies

Each entry in `depends` declares a dependency on another component:

```yaml
depends:
  - on: api
  - on: rds
```

| Field | Required | Description |
|-------|----------|-------------|
| `on` | yes | Name of the component this one depends on. Must exist in `components`. |

Dependencies are directional: `nginx.depends.on: api` means "nginx depends on api" — if api is unhealthy, nginx may be affected.

The engine uses the dependency graph to:

1. Determine probe order (start from outermost, work inward)
2. Build failure paths (trace from symptom to root cause)
3. Eliminate healthy branches (if a dependency is healthy, its sub-tree is cleared)

!!! note "Soft dependencies"
    All dependencies are currently treated as hard — if a dependency is unhealthy, the engine considers the dependent component potentially affected. Soft/optional dependency support (`soft: true`) is planned but not yet implemented. If your system has optional dependencies, model only the hard ones for now.

### Health expressions

The `healthy` field adds conditions beyond the provider's built-in defaults. The provider already defines default health conditions for each type (e.g., `ready_replicas == desired_replicas` for a Kubernetes deployment). Your `healthy` overrides add to these.

```yaml
healthy:
  - connection_count < 500
  - response_time < 1000
```

#### Expression syntax

Each expression follows the pattern:

```
<fact_name> <operator> <value>
```

**Operators:**

| Operator | Meaning |
|----------|---------|
| `==` | equals |
| `!=` | not equals |
| `<` | less than |
| `>` | greater than |
| `<=` | less than or equal |
| `>=` | greater than or equal |

**Values:** integers (`500`), floats (`0.95`), booleans (`true`, `false`), strings (`"running"`).

**Fact names** must be facts defined by the provider for that component's type. See [Type Catalog](type-catalog.md) for which facts each type exposes.

**Compound expressions** (used in provider state definitions, not in model `healthy` fields):

| Syntax | Meaning |
|--------|---------|
| `expr1 & expr2` | both must be true |
| `expr1 \| expr2` | either must be true |

In the model's `healthy` list, each entry is a separate condition. All must hold for the component to be healthy (implicit AND).

---

## Complete example

```yaml
meta:
  name: storefront
  version: "1.0"
  providers:
    - kubernetes
  vars:
    namespace: production

components:
  nginx:
    type: ingress           # from kubernetes provider
    depends:
      - on: frontend
      - on: api

  frontend:
    type: deployment         # from kubernetes provider
    depends:
      - on: api

  api:
    type: deployment         # from kubernetes provider
    depends:
      - on: rds

  rds:
    providers:
      - aws                  # override: use aws provider, not kubernetes
    type: rds_instance       # from aws provider
    healthy:
      - connection_count < 500  # additional condition beyond provider defaults
```

## Validation

```bash
mgtt model validate              # validate the default system.model.yaml
mgtt model validate path/to.yaml # validate a specific file
```

The validator checks:

- All component types exist in the declared providers (or fall back to `generic.component` and emit an `INFO` line, unless `strict_types: true`)
- All dependency targets exist in `components`
- No circular dependencies
- Health expressions reference valid facts for the component's type
- Expression syntax is correct
- `state.triggered_by` labels (declared by providers) have at least one `failure_modes.can_cause` producer — unknown labels raise a warning (unreachable state)
- Existing `scenarios.yaml` sidecar still matches the model + types (source-hash drift) — unless `meta.scenarios: none`
- Duplicate `(type, resource)` tuples under the same provider produce a warning (usually a copy-paste mistake; legitimate when two probes target one resource with different fact sets).

See also:

- [`mgtt model validate --write-scenarios`](cli.md) — regenerate the sidecar after model changes
- [`scenarios.yaml`](scenarios-yaml.md) — the generated sidecar consumed by `mgtt diagnose`
