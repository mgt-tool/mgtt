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
| `--toolset all\|diagnose\|authoring` | `all` | Which tools to serve. `diagnose`: the incident and probe tools. `authoring`: the vocabulary and model-validation tools, which read installed providers and the model they are given, never a live system, and write nothing. A server for writing models, in CI or on a laptop, can run `--toolset authoring` with no credentials at all. `about` reports the active toolset. |

Blocked probes still return the rendered command plus a `status: blocked_<reason>`
tag so the agent can hand off or queue for human approval.

## Tool surface (Phase 1)

| Tool | Purpose |
|------|---------|
| `about` | Server version, transports, safety posture. |
| `incident_start` | Create an incident from a model path. Returns `incident_id`. |
| `incident_end` | Close an incident with optional verdict. |
| `incident_snapshot` | Full diagnostic-memory bundle — scenarios (alive + eliminated), facts, suggested next probe, status. |
| `plan` | Compute the current path tree and suggested next probe. Does not execute. `cannot_rule_out` lists components left undecided because some of their facts could not be read; `root_cause` is only as good as what was seen. `incident_snapshot` carries the same field. |
| `probe` | Render or execute the engine's next suggested probe. `execute=false` renders only. A refused or timed-out probe returns `forbidden` / `transient` and is recorded as an unknown fact; `operator_prompt_required` means the fact has no command and its provider no runner. |
| `fact_add` | Append an observation (agent-collected). |
| `facts_list` | List facts, optionally filtered to one component. |
| `scenarios_list` | All enumerated failure chains for the incident's model. |
| `scenarios_alive` | Chains still consistent with observed facts. |

### Authoring tools

| Tool | Purpose |
|------|---------|
| `types_list` | The component types the installed providers define, with how many facts each exposes. Optional `provider` filter. |
| `types_describe` | One type: its facts and their value types, its default `healthy` rules, its states with `when`, `triggered_by` and `can_cause`, its default state, and the variables its provider declares. The vocabulary to use instead of inventing fact names, and the rules a `healthy:` override replaces. |
| `model_validate` | Every error and warning in a model at once, each with `component`, `field`, `message` and, where there is one, `suggestion`. It covers unknown dependencies, cycles, invalid `need:`, `healthy:` overrides that drop a type rule, rules reading a variable nothing sets, and types no provider defines. Takes `model_path` (a file the server can read) **or** `model_source` (the YAML itself, up to 512 KiB), for clients that cannot place files where the server reads them. |

A draft model reaches the repository as a reviewed change; no tool writes it. `incident_start` takes a path only: diagnosis runs against committed models.

Each tool's JSON schema ships in `internal/mcp/schemas.go`. The server also
sends MCP `instructions` describing the workflow (`incident_start`, then
`plan` / `probe` until a root cause), for clients that pass them to the model.

### Tool names before 0.4

Up to 0.3 the tools were named with dots (`incident.start`, `facts.list`, ...).
Some clients and model APIs accept only `[A-Za-z0-9_-]` in tool names and
reject the whole tool list over one dotted name, so 0.4 uses underscores.
`mgtt mcp serve --legacy-tool-names` also registers the dotted names as
deprecated aliases, for one release, for agents that call them by name.

## State

Persistent incidents live in `$MGTT_HOME` as `<id>.state.yaml` — same
format the CLI uses. A running MCP server and a human running
`mgtt status <id>` see the same incident. One writer per incident via
`sync.Mutex` keyed by `incident_id`; multi-server HA is out of scope —
**one MCP server per `$MGTT_HOME`**.

The MCP path does not touch `.mgtt-current` (the CLI's single-active
pointer), so humans and agents can drive separate incidents in parallel.
