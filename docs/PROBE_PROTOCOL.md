# Probe Protocol

This document is the authoritative contract between mgtt and any provider runner binary. Providers MUST conform. mgtt MUST NOT assume behavior not specified here.

mgtt's engine and CLI talk to providers exclusively through the `Executor` interface. Anything outside this document — backend choice (kubectl, aws, docker, …), authorization model (RBAC, IAM, tokens, …), JSON path conventions — is the provider's concern, not core's.

## On this page

- [Invocation](#invocation) — argv shape
- [Success output](#success-output-stdout-exit-0) — JSON on stdout
- [Error output](#error-output-stderr-non-zero-exit) — exit codes
- [Derived facts](#derived-facts) — `window` and `derive`
- [Timeouts and limits](#timeouts-and-limits)
- [Debug output](#debug-output)
- [Versioning](#versioning) — `requires.mgtt`
- [Validation](#validation)
- [Read-only contract](#read-only-contract)

## Invocation

mgtt invokes the runner as:

    <runner> probe <component-name> <fact-name> [--<key> <value> ...]

- `<runner>` is `runtime.entrypoint` from the provider's `manifest.yaml` (or the convention-default: `bin/mgtt-provider-<name>` for source installs, the image's `ENTRYPOINT` for image installs).
- All entries from `Command.Vars` and `Command.Extra` are passed as `--<key> <value>` pairs in alphabetical order. Core does not privilege any key (no special `--namespace`, `--cluster`, etc.). Providers declare which flags they expect in their own README.
- `--type <T>` is reserved by core when the model declares a typed component.
- `--window <duration>` and `--derive delta|rate|max` are reserved by core for a derived fact (see [Derived facts](#derived-facts)); a model variable with either name is a usage error.
- A key appearing in both `Vars` and `Extra` is a usage error — the runner reports `ErrUsage`.

## Success output (stdout, exit 0)

A single JSON object on stdout, terminated by newline:

    {"value": <typed value or null>, "raw": "<human-readable>", "status": "ok"|"not_found"}

- `value` matches the declared fact type.
- `raw` is a short operator-friendly rendering.
- `status`:
  - `"ok"` — authoritative value.
  - `"not_found"` — the resource does not exist. `value` MUST be null, `raw` MAY be empty. Core translates this to an engine `UnresolvedError` so the operator sees "resource not found" rather than a misleading typed value.

If `status` is omitted, core defaults it to `"ok"` (back-compat with pre-1.1 providers).

## Error output (stderr, non-zero exit)

A single human-readable line on stderr, then exit code per table:

| Exit | Class       | Meaning                                              |
|------|-------------|------------------------------------------------------|
| 0    | success     | Probe succeeded (including not_found)                |
| 1    | usage       | Bad args, unknown type/fact, conflicting Vars/Extra  |
| 2    | env         | Required dependency missing (kubectl, aws CLI, …)    |
| 3    | forbidden   | Authorization rejected                               |
| 4    | transient   | Network, timeout, 5xx — caller may retry             |
| 5    | protocol    | Backend returned malformed data                      |

Core maps exit codes to sentinel errors (`probe.ErrUsage`, `probe.ErrEnv`, `probe.ErrForbidden`, `probe.ErrTransient`, `probe.ErrProtocol`). Providers writing in Go can import the matching sentinel set from `github.com/mgt-tool/mgtt/sdk/provider`.

## Derived facts

A type may declare a fact over a trailing window:

```yaml
facts:
  queue_depth_delta_5m: { type: mgtt.int, window: 5m, derive: delta }
```

Both keys or neither, and only on a numeric fact. Core passes them on every probe of that fact (`--window 5m --derive delta`) and otherwise treats the value like any other: the provider computes it, so the engine stays free of time.

- `delta` is the last sample in the window minus the first.
- `rate` is that per second between them.
- `max` is the largest sample.

A window holding too few samples to tell (one for `delta` or `rate`, none for `max`) is transient, exit 4: the fact is unknown, not zero. The Go SDK's `provider.Windowed` reads a series and does the reduction.

## Timeouts and limits

- Each probe is bounded by `Command.Timeout` (default 30s). On expiry, the runner is sent SIGKILL via context cancellation and the call is reported as `ErrTransient`.
- Stdout larger than 10 MiB is treated as `ErrProtocol`.

## Debug output

When mgtt sets `MGTT_DEBUG=1` in the runner's environment, the provider MAY emit trace lines to stderr. Debug MUST NOT be written to stdout — it would corrupt the JSON contract.

## Versioning

`manifest.yaml` declares `meta.requires.mgtt` as a semver range. **Only `>=X.Y.Z` is accepted**; ranges, carets, and tildes are rejected at load time. Core refuses to load incompatible providers — except for `mgtt provider uninstall <name>`, which always works regardless of version mismatch (you must always be able to remove a provider you can no longer use).

## Validation

Providers should be validated with:

    mgtt provider validate <name>           # static checks, safe in any CI

Checks against a live backend belong in the provider's own CI; core assumes no backend is reachable.

## Read-only contract

`manifest.yaml` declares the write posture at the top level:

```yaml
read_only: true   # default — pure reader. Omit entirely in most providers.
# or:
read_only: false
writes_note: |
  Describe what the provider writes, when, and why. `mgtt provider
  install` prints this note so operators consent knowingly.
```

`read_only: true` is a contract the provider makes with operators. Core cannot enforce it directly — the operator is responsible for binding credentials that match the declaration (kubernetes RBAC, cloud IAM, daemon-socket permissions, scoped tokens, POSIX permissions, or "no credentials needed"). Providers should document the least-privilege credential provisioning for their backend in their README, which is narrative enough to cover the authz model of any particular backend — no one-size structured field fits kubectl RBAC + AWS IAM + Tempo bearer tokens simultaneously.
