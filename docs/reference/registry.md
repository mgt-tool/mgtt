# Provider Registry

<!--
GENERATED FILE — do not edit by hand.
Rebuilt by docs/_hooks/registry_generator.py on every mkdocs build.
-->

Community-maintained providers for mgtt, each pulled from its repo's
`manifest.yaml` at the latest `v*` tag.
The name→URL map in
[`docs/registry.yaml`](https://github.com/mgt-tool/mgtt/blob/main/docs/registry.yaml)
is the single source of truth.
Install a provider by name, by `owner/name@version`, by repo URL, or as a
digest-pinned image, using any line from its card below.

---


## kubernetes

Kubernetes cluster resources — workloads, networking, storage, RBAC, webhooks, and scheduling

- **FQN**: `mgt-tool/kubernetes@2.3.2`
- **Install methods**: —
- **Capabilities**: — · **Network**: — (bridge)
- **Backends**: —
- **Posture**: read-only
- **Tags**: —
- **Requires mgtt**: `>=0.1.2`

```bash
mgtt provider install kubernetes
mgtt provider install mgt-tool/kubernetes@2.3.2
mgtt provider install https://github.com/mgt-tool/mgtt-provider-kubernetes
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-kubernetes:2.3.2@<digest>
```

---
## aws

AWS resources for mgtt — RDS database instances via aws-cli

- **FQN**: `mgt-tool/aws@0.2.0`
- **Install methods**: —
- **Capabilities**: — · **Network**: — (bridge)
- **Backends**: —
- **Posture**: read-only
- **Tags**: databases, cloud, aws
- **Requires mgtt**: `>=0.1.0`

```bash
mgtt provider install aws
mgtt provider install mgt-tool/aws@0.2.0
mgtt provider install https://github.com/mgt-tool/mgtt-provider-aws
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-aws:0.2.0@<digest>
```

---
## docker

Container health checks against a Docker daemon. Use for "is my container running, restarting too much, or healthy per its HEALTHCHECK?" invariants.

- **FQN**: `mgt-tool/docker@0.2.1`
- **Install methods**: —
- **Capabilities**: — · **Network**: — (bridge)
- **Backends**: —
- **Posture**: read-only
- **Tags**: containers, runtime, docker
- **Requires mgtt**: `>=0.1.4`

```bash
mgtt provider install docker
mgtt provider install mgt-tool/docker@0.2.1
mgtt provider install https://github.com/mgt-tool/mgtt-provider-docker
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-docker:0.2.1@<digest>
```

---
## terraform

Terraform-managed infrastructure — state health, resource presence, drift detection, config-vs-reality reasoning

- **FQN**: `mgt-tool/terraform@0.1.1`
- **Install methods**: —
- **Capabilities**: — · **Network**: — (bridge)
- **Backends**: —
- **Posture**: writes
- **Writes**: The `drifted` fact runs `terraform plan` which refreshes state —
- **Tags**: iac, terraform, hashicorp
- **Requires mgtt**: `>=0.1.2`

```bash
mgtt provider install terraform
mgtt provider install mgt-tool/terraform@0.1.1
mgtt provider install https://github.com/mgt-tool/mgtt-provider-terraform
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-terraform:0.1.1@<digest>
```

---
## tempo

Per-span SLO checks against Grafana Tempo. Use for "p99 of <span> stays under <target>" invariants.

- **FQN**: `mgt-tool/tempo@0.2.1`
- **Install methods**: —
- **Capabilities**: — · **Network**: — (bridge)
- **Backends**: —
- **Posture**: read-only
- **Tags**: tracing, otel, grafana, slo
- **Requires mgtt**: `>=0.1.4`

```bash
mgtt provider install tempo
mgtt provider install mgt-tool/tempo@0.2.1
mgtt provider install https://github.com/mgt-tool/mgtt-provider-tempo
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.1@<digest>
```

---
## quickwit

Cross-span tracing checks against Quickwit. Use for transaction-flow completion, async-hop lag, and consumer-pool health invariants.

- **FQN**: `mgt-tool/quickwit@0.1.2`
- **Install methods**: —
- **Capabilities**: — · **Network**: — (bridge)
- **Backends**: —
- **Posture**: read-only
- **Tags**: tracing, otel, quickwit, slo, async
- **Requires mgtt**: `>=0.1.4`

```bash
mgtt provider install quickwit
mgtt provider install mgt-tool/quickwit@0.1.2
mgtt provider install https://github.com/mgt-tool/mgtt-provider-quickwit
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-quickwit:0.1.2@<digest>
```

---
