# Provider Registry

Community-maintained providers for mgtt.

## Official Providers

| Provider | Type | Description | Install |
|----------|------|-------------|---------|
| [kubernetes](https://github.com/sajonaro/mgtt-provider-kubernetes) | ingress, deployment | Kubernetes workloads via kubectl | `mgtt provider install kubernetes` |
| [aws](https://github.com/sajonaro/mgtt-provider-aws) | rds_instance | AWS RDS via aws-cli | `mgtt provider install aws` |

## Community Providers

| Provider | Type | Description | Install |
|----------|------|-------------|---------|
| [docker](https://github.com/sajonaro/mgtt-provider-docker) | container | Docker containers via docker inspect | `mgtt provider install docker` |

## Publishing Your Provider

1. Create a git repository with the [provider structure](../providers/overview.md)
2. Ensure `mgtt provider install <your-repo-url>` works
3. Open a PR to add your provider to this registry

## Registry File

`mgtt provider install <name>` fetches the registry index from GitHub Pages to resolve provider names to git URLs. The registry is also available for programmatic access:

```
https://sajonaro.github.io/mgtt/registry.yaml
```

```yaml
providers:
  kubernetes:
    url: https://github.com/sajonaro/mgtt-provider-kubernetes
    description: Kubernetes workloads via kubectl
    types: [ingress, deployment]
  aws:
    url: https://github.com/sajonaro/mgtt-provider-aws
    description: AWS resources via aws-cli
    types: [rds_instance]
  docker:
    url: https://github.com/sajonaro/mgtt-provider-docker
    description: Docker containers via docker inspect
    types: [container]
```
