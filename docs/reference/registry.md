# Provider Registry

<!--
GENERATED FILE — do not edit by hand.
Source: docs/registry.yaml (minimal name→URL map) + each provider's upstream
manifest.yaml. Rebuilt by docs/_hooks/registry_generator.py on every
mkdocs build.
-->

Community-maintained providers for mgtt.

The single source of truth for the name→URL map is
[`docs/registry.yaml`](https://github.com/mgt-tool/mgtt/blob/main/docs/registry.yaml).
Per-provider detail below is pulled from each repo's `manifest.yaml` at
its latest `v*` tag on every docs build.

Replace `<digest>` shown in Install commands below with the current
`sha256:…` from your own `docker buildx imagetools inspect` if you need
to double-check.

---


## kubernetes

Per-span SLO checks against Grafana Tempo

- **FQN**: `mgt-tool/tempo@0.2.0`
- **Capabilities**: — · **Network**: `host`
- **Posture**: read-only
- **Tags**: tracing, otel
- **Requires mgtt**: `>=0.1.0`

```bash
mgtt provider install kubernetes
mgtt provider install mgt-tool/tempo@0.2.0
mgtt provider install https://github.com/mgt-tool/mgtt-provider-kubernetes
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-kubernetes:0.2.0@sha256:deadbeef
```

---
## aws

Per-span SLO checks against Grafana Tempo

- **FQN**: `mgt-tool/tempo@0.2.0`
- **Capabilities**: — · **Network**: `host`
- **Posture**: read-only
- **Tags**: tracing, otel
- **Requires mgtt**: `>=0.1.0`

```bash
mgtt provider install aws
mgtt provider install mgt-tool/tempo@0.2.0
mgtt provider install https://github.com/mgt-tool/mgtt-provider-aws
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-aws:0.2.0@sha256:deadbeef
```

---
## docker

Per-span SLO checks against Grafana Tempo

- **FQN**: `mgt-tool/tempo@0.2.0`
- **Capabilities**: — · **Network**: `host`
- **Posture**: read-only
- **Tags**: tracing, otel
- **Requires mgtt**: `>=0.1.0`

```bash
mgtt provider install docker
mgtt provider install mgt-tool/tempo@0.2.0
mgtt provider install https://github.com/mgt-tool/mgtt-provider-docker
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-docker:0.2.0@sha256:deadbeef
```

---
## terraform

Per-span SLO checks against Grafana Tempo

- **FQN**: `mgt-tool/tempo@0.2.0`
- **Capabilities**: — · **Network**: `host`
- **Posture**: read-only
- **Tags**: tracing, otel
- **Requires mgtt**: `>=0.1.0`

```bash
mgtt provider install terraform
mgtt provider install mgt-tool/tempo@0.2.0
mgtt provider install https://github.com/mgt-tool/mgtt-provider-terraform
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-terraform:0.2.0@sha256:deadbeef
```

---
## tempo

Per-span SLO checks against Grafana Tempo

- **FQN**: `mgt-tool/tempo@0.2.0`
- **Capabilities**: — · **Network**: `host`
- **Posture**: read-only
- **Tags**: tracing, otel
- **Requires mgtt**: `>=0.1.0`

```bash
mgtt provider install tempo
mgtt provider install mgt-tool/tempo@0.2.0
mgtt provider install https://github.com/mgt-tool/mgtt-provider-tempo
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0@sha256:deadbeef
```

---
## quickwit

Per-span SLO checks against Grafana Tempo

- **FQN**: `mgt-tool/tempo@0.2.0`
- **Capabilities**: — · **Network**: `host`
- **Posture**: read-only
- **Tags**: tracing, otel
- **Requires mgtt**: `>=0.1.0`

```bash
mgtt provider install quickwit
mgtt provider install mgt-tool/tempo@0.2.0
mgtt provider install https://github.com/mgt-tool/mgtt-provider-quickwit
mgtt provider install --image ghcr.io/mgt-tool/mgtt-provider-quickwit:0.2.0@sha256:deadbeef
```

---
