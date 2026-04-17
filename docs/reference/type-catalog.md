# Type Catalog

Every component in a model has a `type` — a type defined by a provider. This page lists all types from the official providers, the facts you can observe, the states the engine can derive, and the failure modes it uses for reasoning.

Use this to write correct `inject` blocks in scenarios and `healthy` overrides in models without needing to install mgtt first.

## On this page

- [Kubernetes provider](#kubernetes-provider)
- [AWS provider](#aws-provider)
- [Docker provider (community)](#docker-provider-community)
- [Stdlib primitive types](#stdlib-primitive-types)
- [Standard failure mode vocabulary](#standard-failure-mode-vocabulary)
- [When the type you need doesn't exist](#when-the-type-you-need-doesnt-exist)

---

## Kubernetes provider

Install: `mgtt provider install kubernetes`

Auth: `KUBECONFIG`, `~/.kube/config`, or in-cluster service account. Read-only access via kubectl.

Variable: `namespace` (default: `default`)

### `ingress`

A Kubernetes ingress or reverse proxy (e.g., nginx ingress controller).

#### Facts

| Fact | Type | Cost | Description |
|------|------|------|-------------|
| `upstream_count` | `mgtt.int` | low | Number of upstream endpoints backing this ingress |

#### Health conditions

```
upstream_count > 0
```

#### States

| State | Condition | Description |
|-------|-----------|-------------|
| `live` | `upstream_count > 0` | Serving traffic normally |
| `draining` | `upstream_count == 0` | No upstream endpoints |

Default active state: `live`

#### Failure modes

| State | Can cause |
|-------|-----------|
| `draining` | `upstream_failure`, `5xx_errors` |

---

### `deployment`

A Kubernetes Deployment — the most common workload type.

#### Facts

| Fact | Type | Cost | Description |
|------|------|------|-------------|
| `ready_replicas` | `mgtt.int` | low | Number of pods in Ready state |
| `desired_replicas` | `mgtt.int` | low | Configured replica count (`.spec.replicas`) |
| `restart_count` | `mgtt.int` | low | Container restart count (highest across pods) |
| `endpoints` | `mgtt.int` | low | Number of endpoint IPs in the Service |

#### Health conditions

```
ready_replicas == desired_replicas
endpoints > 0
restart_count < 5
```

#### States

States are evaluated top-to-bottom — first match wins.

| State | Condition | Description |
|-------|-----------|-------------|
| `degraded` | `ready_replicas < desired_replicas & restart_count > 5` | Crash-looping — pods restarting repeatedly |
| `draining` | `desired_replicas == 0` | Scaled to zero intentionally |
| `starting` | `ready_replicas < desired_replicas` | Pods initializing (not yet crash-looping) |
| `live` | `ready_replicas == desired_replicas` | All replicas ready |

Default active state: `live`

!!! warning "State ordering matters"
    `degraded` must be checked before `starting` because both match when `ready_replicas < desired_replicas`. The difference is `restart_count > 5` — without checking restarts, a crash-looping deployment looks like it's still starting up.

#### Failure modes

| State | Can cause |
|-------|-----------|
| `degraded` | `upstream_failure`, `timeout`, `connection_refused`, `5xx_errors` |
| `draining` | `upstream_failure`, `connection_refused` |
| `starting` | `upstream_failure`, `timeout` |

---

## AWS provider

Install: `mgtt provider install aws`

Auth: `AWS_PROFILE`, `AWS_ACCESS_KEY_ID`+`AWS_SECRET_ACCESS_KEY`, `~/.aws/credentials`, or instance profile. Read-only AWS API access.

### `rds_instance`

An AWS RDS database instance.

#### Facts

| Fact | Type | Cost | Description |
|------|------|------|-------------|
| `available` | `mgtt.bool` | low | Whether the instance is accepting connections |
| `connection_count` | `mgtt.int` | low | Current database connections (from CloudWatch) |

#### Health conditions

```
available == true
connection_count < 500
```

#### States

| State | Condition | Description |
|-------|-----------|-------------|
| `live` | `available == true` | Accepting connections |
| `stopped` | `available == false` | Not accepting connections |

Default active state: `live`

#### Failure modes

| State | Can cause |
|-------|-----------|
| `stopped` | `upstream_failure`, `connection_refused`, `query_timeout` |

---

## Docker provider (community)

Install: `mgtt provider install https://github.com/mgt-tool/mgtt-provider-docker`

### `container`

A Docker container.

See the [provider repository](https://github.com/mgt-tool/mgtt-provider-docker) for the full type definition.

---

## Stdlib primitive types

Every provider fact declares a type from mgtt's built-in stdlib. These are the base types:

| Type | Base | Unit/Range | Example |
|------|------|------------|---------|
| `mgtt.int` | integer | — | `42` |
| `mgtt.float` | float | — | `0.95` |
| `mgtt.bool` | boolean | — | `true` |
| `mgtt.string` | string | — | `"running"` |
| `mgtt.duration` | float | ms, s, m, h, d | `500` (ms) |
| `mgtt.bytes` | integer | b, kb, mb, gb, tb | `1024` |
| `mgtt.ratio` | float | 0..1 | `0.95` |
| `mgtt.percentage` | float | 0..100 | `95.0` |
| `mgtt.count` | integer | 0.. | `12` |
| `mgtt.timestamp` | string | ISO 8601 | `"2024-02-05T07:50:00Z"` |

Inspect at runtime:

```bash
mgtt stdlib ls              # list all primitive types
mgtt stdlib inspect count   # details for a specific type
```

---

## Standard failure mode vocabulary

Providers declare failure modes using a standard vocabulary. These are the recognized terms:

| Failure mode | Meaning |
|--------------|---------|
| `upstream_failure` | Downstream components cannot reach this one |
| `connection_refused` | TCP connections actively rejected |
| `timeout` | Responses too slow or no response |
| `5xx_errors` | HTTP 5xx errors returned to callers |
| `query_timeout` | Database queries timing out |
| `dns_failure` | DNS resolution failing |
| `auth_failure` | Authentication/authorization rejected |
| `resource_exhaustion` | CPU, memory, disk, or connection limits hit |

---

## When the type you need doesn't exist

The catalog above is small — three types across two providers. Real systems have many more component types (ElastiCache clusters, message brokers, S3 buckets, CDNs, secrets stores, etc.). When you need a type that isn't listed, you have three options:

### Option 1: Write a vocabulary-only provider (fastest)

Define a `provider.yaml` with your types, facts, states, and inline probe commands. No compiled binary needed — mgtt executes the shell commands directly.

```yaml
# my-aws-extras/provider.yaml
meta:
  name: my-aws-extras
  version: 0.1.0
  description: Additional AWS types for my project
  requires:
    mgtt: ">=1.0"
  command: ""

hooks:
  install: ""

# read_only defaults to true; shell-fallback vocabulary-only providers
# are inherently read-only. Document the AWS credential chain this
# provider uses in a README rather than in provider.yaml — that's
# narrative that doesn't fit a structured field.

types:
  elasticache_cluster:
    description: AWS ElastiCache Redis/Memcached cluster
    facts:
      available:
        type: mgtt.bool
        ttl: 60s
        probe:
          cmd: "aws elasticache describe-cache-clusters --cache-cluster-id {name} --query 'CacheClusters[0].CacheClusterStatus' --output text"
          parse: bool
          cost: low
          access: AWS API read-only
    healthy:
      - available == true
    states:
      live:
        when: "available == true"
        description: accepting connections
      stopped:
        when: "available == false"
        description: not available
    default_active_state: live
    failure_modes:
      stopped:
        can_cause: [upstream_failure, connection_refused]
```

Install it:

```bash
mgtt provider install ./my-aws-extras
```

This is the right starting point. You can always add a compiled binary later for better performance or more complex probe logic.

### Option 2: Extend an existing provider

Fork the official `aws` or `kubernetes` provider repository and add types to its `provider.yaml`. This keeps all your AWS types in one provider.

### Option 3: Contribute upstream

If your type is broadly useful (e.g., `elasticache_cluster` for the AWS provider), open a PR against the official provider. See [Writing Providers](../providers/overview.md) for the full provider development guide.

---

The key point: **mgtt's type system is open**. The catalog above is what ships today, not what's possible. Any component you can observe via a shell command can be modeled.
