# Provider Registry

Community-maintained providers for mgtt.

| Provider | Covers | Install |
|----------|--------|---------|
| [kubernetes](https://github.com/mgt-tool/mgtt-provider-kubernetes) | workloads, networking, scaling, storage, cluster, prerequisites, rbac, webhooks, extensibility | `mgtt provider install kubernetes` |
| [aws](https://github.com/mgt-tool/mgtt-provider-aws) | databases, compute, messaging, storage | `mgtt provider install aws` |
| [docker](https://github.com/mgt-tool/mgtt-provider-docker) | containers | `mgtt provider install docker` |

Run `mgtt provider inspect <name>` after install to see the full type catalog the provider declares — the registry intentionally shows tags only.

## Publishing Your Provider

1. Create a git repository with the [provider structure](../providers/overview.md).
2. Ensure `mgtt provider install <your-repo-url>` works.
3. Open a PR to add your provider to this registry.

## Registry File

`mgtt provider install <name>` fetches the registry index from GitHub Pages to resolve provider names to git URLs. The index is also available programmatically:

```
https://mgt-tool.github.io/mgtt/registry.yaml
```

```yaml
providers:
  kubernetes:
    url: https://github.com/mgt-tool/mgtt-provider-kubernetes
    description: Kubernetes cluster resources via kubectl
    tags: [workloads, networking, storage, rbac, scaling, webhooks, extensibility]
  aws:
    url: https://github.com/mgt-tool/mgtt-provider-aws
    description: AWS managed services via aws-cli
    tags: [databases, compute, messaging, storage]
```
