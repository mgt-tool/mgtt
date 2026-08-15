# mgtt mcp

Serve mgtt's engine as an MCP (Model Context Protocol) service. Agents call
tools instead of parsing CLI output.

## Transports

### stdio

The MCP client spawns `mgtt` as a subprocess and speaks JSON-RPC over
stdin/stdout. No network, no auth, no config.

Claude Desktop / Claude Code / Cursor / Zed config:

```json
{
  "mcpServers": {
    "mgtt": {
      "command": "mgtt",
      "args": ["mcp", "serve"]
    }
  }
}
```

### streamable HTTP

For CI runners, sidecars, and any setup where the client and server don't
share a process tree.

```bash
export MGTT_MCP_TOKEN=$(openssl rand -hex 32)
mgtt mcp serve --http --listen :8080 --token-env MGTT_MCP_TOKEN
```

Every request requires `Authorization: Bearer $MGTT_MCP_TOKEN`. Missing or
wrong token returns 401 before dispatch. mgtt refuses to start in `--http`
mode when `--token-env` is unset or the named env var is empty.

TLS is the operator's job — terminate at a reverse proxy, sidecar, or ingress.

## Docker sidecar

The image at the repo root runs the same binary.

```bash
docker build -t mgtt .
docker run --rm -p 8080:8080 \
  -e MGTT_MCP_TOKEN=$(openssl rand -hex 32) \
  -v $PWD:/workspace \
  -v $HOME/.mgtt:/data \
  mgtt mcp serve --http --listen :8080 --token-env MGTT_MCP_TOKEN
```

The image exposes `8080` and sets `MGTT_HOME=/data`; mount your model
workspace at `/workspace` and your provider/incident store at `/data`.

## Safety flags

| Flag | Default | Effect |
|------|---------|--------|
| `--readonly-only` | false | Reject any probe whose provider did not declare `read_only: true`. |
| `--on-write pause\|run\|fail` | `run` | Policy when the next probe is write-capable. `fail` blocks with `blocked_write_fail`; `pause` holds for review with `blocked_write_pause`. |
| `--max-execute-per-incident N` | 50 | Refuse further executions once N probes have run for the incident. Agent-added facts don't count. `0` means unlimited. |
| `--probe-timeout SECS` | 30 | Per-probe timeout, max 300. |

Blocked probes still return the rendered command plus a `status: blocked_<reason>`
tag so the agent can hand off or queue for human approval.

## Tool surface (Phase 1)

| Tool | Purpose |
|------|---------|
| `about` | Server version, transports, safety posture. |
| `incident.start` | Create an incident from a model path. Returns `incident_id`. |
| `incident.end` | Close an incident with optional verdict. |
| `incident.snapshot` | Full diagnostic-memory bundle — scenarios (alive + eliminated), facts, suggested next probe, status. |
| `plan` | Compute the current path tree and suggested next probe. Does not execute. |
| `probe` | Render or execute the engine's next suggested probe. `execute=false` renders only. |
| `fact.add` | Append an observation (agent-collected). |
| `facts.list` | List facts, optionally filtered to one component. |
| `scenarios.list` | All enumerated failure chains for the incident's model. |
| `scenarios.alive` | Chains still consistent with observed facts. |

Each tool's JSON schema ships in `internal/mcp/schemas.go`.

## State

Persistent incidents live in `$MGTT_HOME` as `<id>.state.yaml` — same
format the CLI uses. A running MCP server and a human running
`mgtt status <id>` see the same incident. One writer per incident via
`sync.Mutex` keyed by `incident_id`; multi-server HA is out of scope —
**one MCP server per `$MGTT_HOME`**.

The MCP path does not touch `.mgtt-current` (the CLI's single-active
pointer), so humans and agents can drive separate incidents in parallel.
