# Provider Registry

Community-maintained providers for mgtt.

The single source of truth is [`docs/registry.yaml`](https://github.com/mgt-tool/mgtt/blob/main/docs/registry.yaml) in the mgtt repo. `mgtt provider install <name>` resolves names via the registry index at:

```
https://mgt-tool.github.io/mgtt/registry.yaml
```

Each provider below lists its fully-qualified name (FQN) and every install method mgtt supports. Replace `<digest>` with the current `sha256:…` of the image tag (fetch via `docker buildx imagetools inspect <ref>`).

---

## kubernetes

Kubernetes cluster resources via kubectl.

- **FQN**: `mgt-tool/kubernetes@2.3.1`
- **Capabilities**: `kubectl` · **Network**: `host`
- **Tags**: workloads, networking, scaling, storage, cluster, prerequisites, rbac, webhooks, extensibility

```bash
# registry lookup (shortest)
mgtt provider install kubernetes

# fully-qualified name
mgtt provider install mgt-tool/kubernetes@2.3.1

# git URL
mgtt provider install https://github.com/mgt-tool/mgtt-provider-kubernetes

# Docker image
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-kubernetes:2.3.1@sha256:<digest>
```

---

## aws

AWS managed services via aws-cli.

- **FQN**: `mgt-tool/aws@0.2.0`
- **Capabilities**: `aws` · **Network**: `host`
- **Tags**: databases, compute, messaging, storage

```bash
mgtt provider install aws
mgtt provider install mgt-tool/aws@0.2.0
mgtt provider install https://github.com/mgt-tool/mgtt-provider-aws
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-aws:0.2.0@sha256:<digest>
```

---

## docker

Docker containers via docker inspect.

- **FQN**: `mgt-tool/docker@0.2.0`
- **Capabilities**: `docker` · **Network**: — (default bridge)
- **Tags**: containers

```bash
mgtt provider install docker
mgtt provider install mgt-tool/docker@0.2.0
mgtt provider install https://github.com/mgt-tool/mgtt-provider-docker
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-docker:0.2.0@sha256:<digest>
```

---

## terraform

Terraform-managed infrastructure — state health, drift detection, config-vs-reality reasoning.

- **FQN**: `mgt-tool/terraform@0.1.0`
- **Capabilities**: `terraform` · **Network**: `host`
- **Tags**: iac, terraform, drift

Add the capability matching your state backend (`aws` for S3, `gcloud` for GCS, `azure` for Azure Storage) to `$MGTT_HOME/capabilities.yaml`; Terraform Cloud and local backends need no extra capability.

```bash
mgtt provider install terraform
mgtt provider install mgt-tool/terraform@0.1.0
mgtt provider install https://github.com/mgt-tool/mgtt-provider-terraform
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-terraform:0.1.0@sha256:<digest>
```

---

## tempo

Per-span SLO checks against Grafana Tempo — `current_p99`, `breach_duration`, `error_rate`.

- **FQN**: `mgt-tool/tempo@0.2.0`
- **Capabilities**: — · **Network**: `host`
- **Tags**: tracing, otel, grafana, slo

```bash
mgtt provider install tempo
mgtt provider install mgt-tool/tempo@0.2.0
mgtt provider install https://github.com/mgt-tool/mgtt-provider-tempo
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0@sha256:<digest>
```

---

## quickwit

Cross-span tracing checks against Quickwit — `transaction_flow`, `async_hop`, `consumer_health`.

- **FQN**: `mgt-tool/quickwit@0.1.1`
- **Capabilities**: — · **Network**: `host`
- **Tags**: tracing, otel, quickwit, slo, async

```bash
mgtt provider install quickwit
mgtt provider install mgt-tool/quickwit@0.1.1
mgtt provider install https://github.com/mgt-tool/mgtt-provider-quickwit
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-quickwit:0.1.1@sha256:<digest>
```

---

## Reading the entries

- **FQN** — the fully-qualified name, format `<namespace>/<name>@<version>`. See [Provider Names and Versions](../concepts/provider-fqn-and-versions.md).
- **Capabilities** — what the provider declares in `needs:`. Image installs forward these as `docker run` bind mounts and env vars. See [Provider Capabilities](./image-capabilities.md).
- **Network** — declared `network:` mode when non-default (`host` or `none`); blank means bridge.

Four install methods, same result on disk:

1. **Registry name** — shortest. Requires network access to the registry.
2. **FQN with version** — pins by semver.
3. **Git URL** — bypasses the registry. Works for forks, private mirrors.
4. **`--image`** — no Go toolchain on the host. Digest required.

Run `mgtt provider inspect <name>` after install to see the type catalog, states, and failure modes.

---

## Publishing your provider

1. Create a git repo following the [provider structure](../providers/overview.md).
2. Confirm `mgtt provider install <your-repo-url>` works.
3. Open a PR adding your entry to [`docs/registry.yaml`](https://github.com/mgt-tool/mgtt/blob/main/docs/registry.yaml). The convenience listing above and the programmatic index both come from that file.
