![mgtt](docs/images/mgtt_full_lockup.png)

## Your architecture, executable.

You already draw the diagram — components, dependencies, what "healthy" means. mgtt reads it as code. One YAML model drives two modes:

- **At design time**, `mgtt simulate` injects synthetic failures and asserts the engine reaches the right conclusion. Runs in CI on every PR. Catches architectural drift before the diagram lies to you.
- **At 3am**, `mgtt diagnose` runs the same engine against the live system. Real probes replace the synthetic facts. It names the broken component, eliminates the healthy ones, and hands you the chain.

Same model. Same reasoning. Two fixture sources.

```
$ mgtt diagnose --suspect api

  ▶ probe nginx upstream_count       ✗ unhealthy
  ▶ probe api ready_replicas         ✗ unhealthy
  ▶ probe rds available              ✓ healthy  ← eliminated
  ▶ probe frontend ready_replicas    ✓ healthy  ← eliminated

  Root cause: api.degraded
  Chain:      nginx ← api
  Probes run: 4
```

The engine picks probes by information value, so every call rules out a branch. You didn't need to know the system — the model knew it for you. Partial visibility (RBAC refusals, transient throttles) surfaces as a flag, not an abort.

## Architecture

```mermaid
%%{init: {'flowchart': {'curve': 'stepAfter', 'nodeSpacing': 50, 'rankSpacing': 80}}}%%
flowchart LR
    Human["human"]
    Agent["LLM agent"]

    subgraph Core["mgtt core &nbsp; · &nbsp; no creds"]
        direction TB
        CLI["CLI"]
        MCP["MCP server"]
        Eng["engine"]
        Scen[("scenarios.yaml")]
        Model[("model.yaml")]
        CLI --> Eng
        MCP --> Eng
        Model --> Eng
        Scen -.->|simulate mode| Eng
    end

    subgraph Adapters["adapters &nbsp; · &nbsp; hold creds"]
        direction TB
        K["kubernetes"]
        A["aws"]
        D["docker"]
        T["terraform / tempo / quickwit / ..."]
    end

    subgraph SUT["system under test"]
        direction LR
        NGX["nginx"] --> API["api"] --> RDS[("rds")]
        API --> Cache["redis"]
    end
    Reg["registry<br/>(registry.yaml)"]

    Human --> CLI
    Agent --> MCP
    Eng ==>|probe request| Adapters
    Adapters ==>|parsed facts| Eng
    Adapters ==>|commands| SUT
    SUT ==>|stdout| Adapters
    Reg -.->|mgtt provider install| Adapters
```

mgtt core reasons; adapters translate to backend-specific commands; the registry publishes them. Credentials live only in the adapter layer — the engine never touches them. See [How It Works](./docs/concepts/how-it-works.md#architecture-at-a-glance) for the full picture with tracing backend, queue, and simulation-mode detail.

## Install

```bash
curl -sSL https://raw.githubusercontent.com/mgt-tool/mgtt/main/install.sh | sh
```

Or: `go install github.com/mgt-tool/mgtt/cmd/mgtt@latest`

## Quick start

```bash
mgtt init                          # scaffold system.model.yaml
mgtt model validate                # check the model
mgtt simulate --all                # run scenarios (in CI)
mgtt diagnose --suspect api        # troubleshoot a live system
```

## Docs

- [Quick Start](./docs/getting-started/quickstart.md) — end-to-end in five minutes
- [Blue/green storefront worked example](./docs/examples/blue-green-storefront.md) — 20-component real system, five scenarios, lessons from real use
- [How It Works](./docs/concepts/how-it-works.md) — the constraint engine
- [Docs site](https://mgt-tool.github.io/mgtt) — reference, providers, specs

## For TLA+ users

TLA+ checks your design; mgtt checks your running system.

## License

Dual-licensed: engine + CLI under [AGPL-3.0](LICENSE); provider SDK under [Apache-2.0](sdk/provider/LICENSE).
