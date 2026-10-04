# AI agents (MCP)

`mgtt mcp serve` exposes the engine as MCP tools. The agent never decides which command to run. It asks the engine for the next probe and runs it within the limits you set, so its reasoning is the same reasoning your scenarios test.

## Connect

Claude Code:

```bash
claude mcp add mgtt -- mgtt mcp serve --readonly-only --on-write fail --max-execute-per-incident 20
```

Other clients (`.mcp.json`, Claude Desktop, Cursor, Zed):

```json
{
  "mcpServers": {
    "mgtt": {
      "command": "mgtt",
      "args": ["mcp", "serve", "--readonly-only", "--on-write", "fail"],
      "env": { "MGTT_HOME": "/home/me/.mgtt" }
    }
  }
}
```

The server runs probes with **its own** environment: providers under `$MGTT_HOME`, plus your kubeconfig and AWS profile. Incident files land in its working directory.

For CI runners and sidecars, use HTTP with a bearer token. Terminate TLS in front of it.

```bash
export MGTT_MCP_TOKEN=$(openssl rand -hex 32)
mgtt mcp serve --http --listen :8080 --token-env MGTT_MCP_TOKEN --readonly-only
```

The image `ghcr.io/mgt-tool/mgtt` runs the same command. Mount the model at `/workspace` and `$MGTT_HOME` at `/data`.

## Guardrails

| Flag | Default | Recommended |
|---|---|---|
| `--readonly-only` | off | **on**: refuse providers not declared read-only |
| `--on-write run\|pause\|fail` | `run` | **`fail`**, or `pause` to have a human run write probes |
| `--max-execute-per-incident N` | 50 | about 20 |
| `--probe-timeout SECS` | 30 | |

The MCP server's defaults are looser than `diagnose`'s, so set these flags explicitly. In Claude Code, you can also allow `plan` and `incident_snapshot` without asking and keep a prompt on `probe`.

## The loop

```text
incident_start {model_ref: "/repo/system.model.yaml", suspect: ["api"]}
  → {incident_id: "inc-…"}
plan {incident_id}
  → {suggested: {component: "api", fact: "restart_count",
                 rendered_command: "kubectl -n production get pods …"}, paths: […]}
probe {incident_id, execute: true}
  → {status: "executed", component: "api", fact: "restart_count", value: 47}
… plan / probe until …
plan {incident_id}
  → {root_cause: "rds", cannot_rule_out: [], redundancy_degraded: []}
incident_end {incident_id, verdict: "rds stopped by maintenance window", emit_scenario: true}
  → {saved: true, scenario_path: "scenarios/inc-….yaml", scenario_passes: true}
```

- `probe` only runs the engine's suggestion. `execute: false` returns the rendered command without running it.
- To record something learned elsewhere (logs, dashboards, a human), the agent calls `fact_add {incident_id, component, key, value, note}`.
- `model_ref` resolves against the server's working directory. Use absolute paths.
- `emit_scenario` writes the regression scenario described in the [quick start](../getting-started/quickstart.md#6-retrospective). The agent can open a PR with it.

## Probe statuses

| `status` | Agent should |
|---|---|
| `executed` | continue with `plan` |
| `rendered` | show the command, or call again with `execute: true` |
| `not_found`, `forbidden`, `transient` | continue; the fact is recorded as missing or unknown and the engine accounts for it |
| `blocked_readonly`, `blocked_write_fail`, `blocked_budget` | stop and report; widening the limits is a human decision |
| `blocked_write_pause` | hand the rendered command to a human, then `fact_add` the result |
| `operator_prompt_required` | the fact has no command; ask a human, then `fact_add` |
| `no_suggestion` | call `plan`: it's solved, or stuck because the model has a gap |
| `error` | report `raw` |

## All tools

`about` (version and guardrails), `incident_start`, `plan`, `probe`, `fact_add`, `facts_list`, `incident_snapshot` (everything in one call), `scenarios_list`, `scenarios_alive`, `incident_end`. Each tool's schema is served over `tools/list`. Tool names use underscores; `--legacy-tool-names` also registers the old dotted names.
