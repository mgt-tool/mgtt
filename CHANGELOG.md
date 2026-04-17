# Changelog

All notable changes to mgtt are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Provider capabilities.** Providers declare semantic needs at the top level of `manifest.yaml` (`needs: [kubectl, network]`); mgtt's image runner expands each label into the matching `docker run` bind mounts and env forwards at probe time (git installs inherit them from the operator's shell). Built-in vocabulary covers `network`, `kubectl`, `aws`, `docker`, `terraform`, `gcloud`, `azure`. Operators override or extend via `$MGTT_HOME/capabilities.yaml` (+ `capabilities.d/*.yaml` shards) or `MGTT_IMAGE_CAP_<NAME>` env vars; `MGTT_IMAGE_CAPS_DENY=docker,aws` refuses capabilities regardless of declaration. Install-time prints declared caps for audit. `mgtt provider ls` shows a caps column per installed provider. Validation rejects unknown caps and refuses `needs` on shell-fallback providers. See `docs/reference/image-capabilities.md`.

### Breaking

- Schema: capabilities moved from `image.needs: [...]` to a top-level `needs: [...]` block in `manifest.yaml`. The underlying requirement ("this provider needs kubectl") is a property of the provider, not of the image-install runtime that happens to translate it into flags. Pre-1.0, ripped without an alias — providers must update their YAML.
- Schema: `network` split out of the capability vocabulary into its own top-level field. Previously `needs: [kubectl, network]` conflated two categories — host-resource grants and docker-run isolation mode. Now: `needs: [kubectl]` (tools/creds only) plus `network: host` (or `bridge`/`none`). `mgtt provider validate` rejects `network` as a cap name and validates the new field against the `bridge|host|none` set. All six registry providers updated.
- Schema: `auth:` block ripped. `auth.strategy` / `auth.reads_from` / `auth.access.probes` were freeform prose that mgtt never parsed or enforced — pretending they were structured data hurt more than it helped. `auth.access.writes` had a provider-invented string vocabulary (`"none"`, `"state-refresh-on-plan"`, etc.) that surfaced only as a validation WARN. Replaced with two fields at provider-yaml top level: `read_only: true` (default) or `false` plus a required `writes_note:` prose when `false`. `mgtt provider validate` now enforces the pair; `mgtt provider install` prints the note so operators consent knowingly. Credentials the provider reads (which env vars, which config paths) move out of manifest.yaml and into each provider's README, where they can be narrative and accurate.

### Fixed

- **`mgtt provider install --image` now works with distroless and scratch-based provider images.** `ExtractManifest` previously shelled out to `docker run --rm --entrypoint cat <image> /manifest.yaml`, which required the provider image to ship a `cat` binary on `PATH`. Switched to `docker create` + `docker cp <cid>:/manifest.yaml -` + `docker rm`, decoding the resulting tar stream in-process. Nothing inside the container is executed, so any base image works. No docs, flags, or on-disk state change.
- **`mgtt provider install --image` now extracts `/types/` for multi-file providers.** The installer previously copied only `/manifest.yaml` out of the image. Providers with a `types/<name>.yaml` layout (kubernetes, tempo, quickwit, terraform) would land in `~/.mgtt/providers/<name>/` with no type definitions — `mgtt provider inspect` would report zero types and planning against the provider would silently skip its components. `installFromImage` now calls the new `DockerCmd.ExtractTypes`, which `docker cp`s the `/types/` directory out of the image and writes each `.yaml` entry into `destDir/types/`. Absence of `/types/` is not an error (inline-types providers still work).

## [0.1.4] — 2026-04-16

Configuration story for corporate operators. No new config file — env vars and one CLI flag.

### Added

- **`MGTT_REGISTRY_URL=disabled` / `none` / `off`** (case-insensitive) — skips registry resolution entirely. `mgtt provider install` accepts only git URLs / local paths; bare names produce a clear actionable error wrapped around `registry.ErrRegistryDisabled` so callers can `errors.Is` it.
- **`MGTT_REGISTRY_URL=file:///path/to/registry.yaml`** — load the registry from a local file (RFC 8089 form: absolute path or `localhost` host; percent-decoding honored). For air-gapped installs that ship the index alongside.
- **`--registry <url>` flag** on `mgtt provider install` overrides the env var per-invocation. Precedence: flag > env > default.
- **`MGTT_PROBE_TIMEOUT=60s`** is now actually wired (was documented but not read by any code). Unparseable values emit a one-time stderr warning rather than silently using the default.
- **`docs/reference/configuration.md`** — single canonical reference for every `MGTT_*` env var, with corporate scenarios (air-gap, internal mirror, k8s deployment env block) and an explicit "no telemetry / no auto-update" security-review section.

### Fixed (review-driven, before tag)

- **Cache poisoning across registry sources.** The registry cache is now keyed on `sha256(URL)[:8]` and lives at `$MGTT_HOME/cache/registry/<hash>.yaml`. Switching `MGTT_REGISTRY_URL` no longer serves content fetched against a different URL identity. Critical for shared `MGTT_HOME` multi-tenant installs.
- **Cache root** now honors `MGTT_HOME` (was hardcoded to `$HOME/.mgtt/cache/`).
- **`file://` URL parsing** uses `net/url.Parse` instead of `strings.TrimPrefix` — supports `file:///path` and `file://localhost/path`, rejects other authorities, percent-decodes paths.
- **CLI errors wrap `registry.ErrRegistryDisabled` with `%w`** so the sentinel survives the full error chain.

### Internal

- `registry.Fetch` signature changed from `Fetch(noCache bool)` to `Fetch(Source{URL, NoCache})`. Internal-only; SDK and external providers unaffected.

## [0.1.3] — 2026-04-16

### Added

- **`mgtt provider uninstall <name>`** — runs the provider's optional `hooks.uninstall` script, then removes `~/.mgtt/providers/<name>/`. Uses `LoadEmbedded` (not `LoadForUse`) so version-incompatible providers are always removable. If `manifest.yaml` is malformed, the directory is still removed. If the uninstall hook fails, the directory is still removed. Tests cover all four paths.
- **`hooks.uninstall`** field parsed from `manifest.yaml` alongside `hooks.install`. Providers declare their cleanup script; mgtt wires it.
- **Terraform provider** added to the registry (`docs/registry.yaml`).

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
