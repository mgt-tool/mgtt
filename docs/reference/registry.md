# Provider Registry

Community-maintained providers for mgtt.

The **single source of truth** is [`docs/registry.yaml`](https://github.com/mgt-tool/mgtt/blob/main/docs/registry.yaml) in the mgtt repo. The table below is a convenience snapshot — if it's out of date, the YAML file wins.

`mgtt provider install <name>` fetches the registry index from GitHub Pages to resolve provider names to git URLs:

```
https://mgt-tool.github.io/mgtt/registry.yaml
```

## Current providers

<!-- To update: read docs/registry.yaml and mirror here. -->

| Provider | Description | Tags | Install |
|----------|-------------|------|---------|
| [kubernetes](https://github.com/mgt-tool/mgtt-provider-kubernetes) | Kubernetes cluster resources via kubectl | workloads, networking, scaling, storage, cluster, prerequisites, rbac, webhooks, extensibility | `mgtt provider install kubernetes` |
| [aws](https://github.com/mgt-tool/mgtt-provider-aws) | AWS managed services via aws-cli | databases, compute, messaging, storage | `mgtt provider install aws` |
| [docker](https://github.com/sajonaro/mgtt-provider-docker) | Docker containers via docker inspect | containers | `mgtt provider install docker` |
| [terraform](https://github.com/mgt-tool/mgtt-provider-terraform) | Terraform-managed infrastructure — state health, drift detection, config-vs-reality reasoning | iac, terraform, drift | `mgtt provider install terraform` |

Run `mgtt provider inspect <name>` after install to see the full type catalog.

## Publishing your provider

1. Create a git repo with the [provider structure](../providers/overview.md).
2. Ensure `mgtt provider install <your-repo-url>` works.
3. Open a PR adding your entry to [`docs/registry.yaml`](https://github.com/mgt-tool/mgtt/blob/main/docs/registry.yaml) — the table above and the programmatic index are both derived from that one file.
