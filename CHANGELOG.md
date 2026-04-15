# Changelog

All notable changes to mgtt are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.2] — 2026-04-16

Restores `Request.Namespace` as a struct field for SDK back-compat; v0.1.1
accidentally broke existing providers that accessed `req.Namespace` directly.
The field is now a pure convenience — populated alongside `Extra["namespace"]`
when the flag is present. Core still does not default or privilege it.

## [0.1.1] — 2026-04-16

Adversarial review fixes. Protocol contract and layering invariants tightened; new correctness gates.

### Fixed

- **Layering (SDK)** — `Request.Namespace` is no longer a reserved struct field; the SDK stopped privileging the `--namespace` flag and no longer defaults it to `"default"`. All non-`--type` flags land uniformly in `Request.Extra`. `Request.Namespace()` is now a convenience accessor over `Extra["namespace"]`.
- **Engine** — `status: not_found` results are now recorded in the fact store with `Value: nil`. The engine's `expr` layer converts that into an `UnresolvedError` so the planner doesn't suggest the same probe in a loop.
- **Loader** — `CheckCompatible` now gates every use-path (`plan`, `simulate`, `status`, `model validate`, `provider inspect`) via new `LoadForUse` / `LoadAllForUse` helpers. Incompatible providers can still be seen by `ls` / future `provider uninstall`.
- **Runner** — provider runners are now started in their own process group; timeout expiry sends `SIGKILL` to the entire subtree, so forked `kubectl`/`aws`/... children don't orphan. The old test that required `exec sleep` is gone — the runner handles real forking children.
- **Runner** — unknown `Result.Status` values (anything other than `""`, `"ok"`, `"not_found"`) are rejected with `ErrProtocol`. Previously silently coerced to `"ok"`.
- **Fixture executor** — parse errors no longer pair with an affirmative `Status: "ok"`; the Status is left unset on the error path.
- **Tracer** — writes to the shared stderr are now serialized under a mutex. Safe for concurrent probes.
- **Runner constructor** — `NewExternalRunner` now returns `Executor` (the interface). No concrete type leaks from the public API.
- **`mgtt provider validate`** now checks: absolute `meta.command` paths exist on disk; `healthy:` expressions reference declared facts; `state.when:` expressions reference declared facts; `failure_modes` keys reference declared states.
- **Probe protocol errors** — `fmt.Errorf("%w: ... %v", ...)` paths switched to `%w` throughout so `errors.Is` chains traverse correctly.

### Unchanged

Wire protocol, SDK import path, and existing fixtures remain compatible. Providers built against v0.1.0 keep working; the SDK API surface is backward-compatible (removed fields have accessor replacements).

## [0.1.0] — 2026-04-16

Probe protocol v1 lifted into core. Providers stop reinventing plumbing.

### Added

- **`docs/PROBE_PROTOCOL.md`** — authoritative wire contract between mgtt and provider runners. Single source of truth; per-provider docs reference it instead of restating it.
- **`Result.Status` field** with values `ok` / `not_found`. Engine translates `not_found` into a user-visible "resource not found" message rather than swallowing it as an error or storing a misleading nil value.
- **Sentinel error taxonomy** in `internal/providersupport/probe`: `ErrUsage`, `ErrEnv`, `ErrForbidden`, `ErrTransient`, `ErrProtocol`, `ErrUnknown`. Mapped from runner exit codes per the protocol.
- **`Command.Extra` map** — arbitrary `--key value` flags pass through to the runner. Unblocks providers that need backend-specific flags (CRD GVK, region, cluster, etc.) without core knowing the keys.
- **`Command.Timeout` enforcement** in `ExternalRunner`. The field was previously parsed but ignored.
- **`MGTT_DEBUG=1` tracer** — context-threaded probe-boundary trace lines on stderr. Format prints `vars=N extra=N` counts, not key names — backend vocabulary stays out of core diagnostics.
- **`sdk/provider`** Go SDK — `Registry`, `Main`, `Result` helpers, sentinel errors. External providers `go get github.com/mgt-tool/mgtt/sdk/provider` and write a runner in ~20 lines.
- **`sdk/provider/shell`** — generic backend-CLI helper with timeout, size cap, and pluggable `Classify` for stderr → sentinel error mapping. Default classifier handles only "binary not on PATH"; providers supply their own backend-specific classifier.
- **`meta.requires.mgtt`** semver gating in the loader. Constraint grammar is intentionally `>=X.Y.Z` only; ranges/carets/tildes are rejected at load time. Use-paths gate at executor construction; the future uninstall path bypasses the gate so incompatible providers remain removable.
- **`mgtt provider validate <name>`** — static correctness checks: meta fields, `auth.access.writes`, `requires.mgtt` satisfaction, default state references, fact probe.cmd presence. `--live` validation against a real backend is intentionally not in core; provider repos own that step in their own CI.

### Changed

- **`Mux.Runners`** is now `map[string]Executor` (was `map[string]*ExternalRunner`). Tests, future in-process runners, and any alternate `Executor` implementations now plug in uniformly.
- **`ExternalRunner.Run`** no longer hardcodes `--namespace` from `cmd.Vars["namespace"]`. All `Vars` and `Extra` entries are passed as `--<key> <value>` flags in alphabetical order. Key collisions between `Vars` and `Extra` are rejected as `ErrUsage`. The kubernetes-specific `namespace` concept moves entirely into the kubernetes provider.
- **Fixture executor** defaults `Result.Status` to `StatusOk` on successful parse. New optional per-entry `status: not_found` field models missing-resource scenarios.
- **VERSION** bumped from 0.0.6 → 0.1.0 to reflect the protocol minor.

### Removed

Nothing user-visible. Internal type-name shuffling is documented in commit messages.

### Migration

Providers built against pre-0.1 mgtt continue to work: omitted `Result.Status` defaults to `ok`. Providers that want to use `Command.Extra` must declare `requires: { mgtt: ">=0.1.0" }`.
