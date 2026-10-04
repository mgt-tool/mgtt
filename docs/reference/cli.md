# CLI and configuration

Commands that take a model default to `system.model.yaml` in the current directory. Use `--model <path>` to point elsewhere; `model validate` and `model export` take the path as an argument instead.

## Model

| Command | |
|---|---|
| `mgtt init` | scaffold `system.model.yaml` |
| `mgtt model validate [path]` | check structure, types, references and health rules, and that `scenarios.yaml` is current. `--write-scenarios` regenerates it; `--check-scenarios` checks only that |
| `mgtt model build` | generate the model from provider discovery. `--allow-deletes`, `--tombstone a,b`, `--output` |
| `mgtt model export --json [path]` | the resolved model as JSON, for external checkers |
| `mgtt visualize` | write a Mermaid graph to `model-graph.md` (`--output` to change) |

## Simulate

`mgtt simulate --all` runs `scenarios/*.yaml`. Other flags:

- `--scenario <file>`: run one scenario.
- `--scenarios-dir <dir>`: read scenarios from another directory.
- `--from-scenarios`: check every enumerated chain.
- `--fuzz N` (with `--fuzz-seed`): check conclusions from random partial evidence.

## Incident

| Command | |
|---|---|
| `mgtt incident start` | open an incident (`--id`, `--model`); state goes to `<id>.state.yaml` |
| `mgtt diagnose` | probe until root cause, budget or deadline |
| `mgtt plan` | the same loop, asking before each probe (`--component` to start there) |
| `mgtt fact add <component> <key> <value>` | record your own observation (`--note`) |
| `mgtt status`, `mgtt ls [components\|facts]` | current health and facts |
| `mgtt incident end` | close the incident. `--emit-scenario` writes `scenarios/<id>.yaml`; `--suggest-scenarios` reports chains the model lacks |

`diagnose` flags:

| Flag | Default | |
|---|---|---|
| `--suspect api,rds/stopped` | | a hint, not a filter |
| `--max-probes` | 20 | |
| `--deadline` | 5m | |
| `--readonly-only` | true | run only read-only providers |
| `--on-write pause\|run\|fail` | pause | when the next probe would write |

## Providers

`mgtt provider install <name…|url|path>` (`--image <ref@sha256:…>`, `--registry <url\|file://…\|off>`, `--no-cache`), plus `ls`, `inspect <name> [type]`, `validate <name>` and `uninstall <name>`. `mgtt stdlib ls|inspect` lists the built-in fact types.

## MCP

`mgtt mcp serve`, with the flags described in [AI agents](../guides/agents.md).

## Exit codes

`0` success · `1` validation, simulate or diagnose failure · `2` usage error · `3` internal panic (please report it)

## Environment

| Variable | |
|---|---|
| `MGTT_HOME` | providers and caches (default `~/.mgtt`) |
| `MGTT_REGISTRY_URL` | registry index; `off`, or `file://…` for an air-gapped mirror |
| `MGTT_PROBE_TIMEOUT` | per-probe timeout (`45s`, `2m`) |
| `MGTT_FIXTURES` | replay recorded probe output (`provider → component → fact → {stdout, exit, status}`) instead of probing |
| `MGTT_DEBUG=1` | trace each probe on stderr |
| `MGTT_IMAGE_CAP_<NAME>`, `MGTT_IMAGE_CAPS_DENY` | override or refuse image capabilities |
| `HTTPS_PROXY`, `NO_PROXY`, `SSL_CERT_FILE` | respected by registry fetches and git clones |
