# Model Schema Reference

Complete reference for `system.model.yaml` — the file that describes your system.

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
  providers:                # required — default providers for all components
    - <provider-name>
  vars:                     # optional — variables substituted into probe commands
    <key>: <value>

components:
  <component-name>:
    type: <type-name>       # required — a type defined by a provider
    providers:              # optional — override meta.providers for this component
      - <provider-name>
    depends:                # optional — components this one depends on
      - on: <component-name>
    healthy:                # optional — additional health conditions
      - <expression>
```

---

## `meta` section

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | System name. Used in output and state file naming. |
| `version` | yes | Model version. Quoted string — `"1.0"`, not `1.0`. |
| `providers` | yes | List of provider names. These are the default providers for all components. Install with `mgtt provider install <name>`. |
| `vars` | no | Key-value pairs substituted into probe commands as `{key}`. Common: `namespace`, `region`, `cluster`. |

## `components` section

Each key under `components` is the component name. Names must be unique within the model.

| Field | Required | Description |
|-------|----------|-------------|
| `type` | yes | A type defined by one of the listed providers. See [Type Catalog](type-catalog.md) for available types. |
| `providers` | no | Override `meta.providers` for this component. Use when a component belongs to a different provider (e.g., an AWS RDS database in a Kubernetes-heavy model). |
| `depends` | no | List of dependency entries. See [Dependencies](#dependencies) below. |
| `healthy` | no | Additional health conditions beyond the provider's defaults. See [Health expressions](#health-expressions) below. |

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

- All component types exist in the declared providers
- All dependency targets exist in `components`
- No circular dependencies
- Health expressions reference valid facts for the component's type
- Expression syntax is correct
