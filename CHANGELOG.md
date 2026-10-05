# Changelog

All notable changes to mgtt are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Breaking (MCP)

- **MCP tool names use underscores:** `incident_start`, `incident_end`, `incident_snapshot`, `fact_add`, `facts_list`, `scenarios_list`, `scenarios_alive` (`about`, `plan`, `probe` are unchanged). Some clients and model APIs accept only `[A-Za-z0-9_-]` in tool names and rejected the whole tool list over the dotted ones. Agents discover names from `tools/list`, so most need no change; for one that calls the old names directly, `mgtt mcp serve --legacy-tool-names` registers them as deprecated aliases for one release.

### Added

- **writ verification reaches models on the official provider types.** mgtt2writ now matches `can_cause` against `triggered_by` by mgtt's own rule, under which a failure state that lists no `triggered_by` takes any label. The kubernetes and aws types list none, so until now their models relayed no failure in writ. `make downstream` checks `examples/redundant-web` exhaustively: 648 situations, no health rule disagreeing with a state, and the group holding as it does in simulate. The storefront remains out of reach of exhaustive checking: its components' states multiply to about 6 × 10⁹ situations.
- **writ verification honours redundancy groups.** mgtt2writ now reads the `group` and `need` that `model export` carries on each grouped edge: a member's failure reaches the dependent only once the group can no longer hold, the line the scenarios draw, so `writ check` stops reporting breakages the redundancy prevents. `make downstream` exports minishop with its store doubled using this mgtt and checks with the real writ that one store down cannot take the service down and both can.
- **`mgtt model build --dry-run` and MCP `model_discover`: what discovery would change, without writing.** Both run every provider's discovery and propose the model it implies, merged with the existing one so authored components survive, and report what is added, which discovered components are no longer found (a real build refuses to drop them without `--allow-deletes` or `--tombstone`), the authored components kept, dangling dependencies, and providers whose discovery failed. `model_discover` also returns the proposed YAML. It is in the diagnose toolset only, since discovery reads live systems through the providers' credentials.
- **`model validate` catches healthy rules that disagree with the type's states.** A type defines health twice -- its rules and its default state -- and when an override makes them part company, simulate (states) and diagnose (rules) read the same facts differently; until now only writ verification found it. A bounded search over fact values around every constant the rules and states compare with finds a witness and warns: on minishop's drifted model, `healthy rules hold in state saturated (e.g. available=true, connection_count=500)`, the case writ reports. Only disagreements a component's own override introduces are reported; `healthy_diverges_from: [state]` acknowledges a deliberate one (the storefront's single-replica opensearch). `mgtt provider validate` reports the disagreements in a type's own defaults: today 35 in the kubernetes provider and 9 in aws, left for provider fixes.
- **`mgtt model diff` and MCP `model_diff`: what a model change means.** Compares two revisions by meaning: components added and removed; per component, dependency (with `need:` and `while:`), effective health rule (type defaults and overrides together) and var changes; and, what a text diff cannot show, which user-facing symptoms each component's failure gains or loses. Respelling a rule, or switching a bare `healthy:` list to `replace:` with the same rules, is no change. `--base <git-rev>` compares the model against a revision, for model pull requests. Against its first committed version, the storefront diff lists exactly the live-color guards and the two restored `available == true` rules.
- **`mgtt model impact <component>` and MCP `model_impact`: what breaks if this fails.** The command walks the failure graph from a component's failure states, by the rules the scenarios follow, and lists every component reached with a shortest chain, marks the user-facing symptoms, names where a redundancy group stops the failure, and, when every chain to a component crosses a `while:` guard, lists the guards it breaks under. On the storefront: Redis down reaches 12 components and 3 symptoms whichever color is live; nginx-green down reaches users only while the selector names green.
- **MCP: `scenario_simulate` and `guide`, completing the authoring loop.** `scenario_simulate` runs scenarios against a model with no live system, model and scenarios each by path or inline (several scenarios as YAML documents separated by `---`), and returns each verdict with the expected conclusion beside the actual one. `guide` serves short notes, embedded in the binary, on the authoring loop, `healthy:` overrides, redundancy groups versus `while:` guards, patterns from real models, and the scenario archetypes, so a client learns the lessons before the model teaches them. `make downstream` runs the whole loop over stdio against the real providers: the storefront model and its nine scenarios inline, all passing.
- **MCP authoring tools.** `types_list` and `types_describe` expose the installed providers' vocabulary (facts with value types, default healthy rules, states with `when` / `triggered_by` / `can_cause`, declared variables), so a client writes models from real fact names instead of plausible ones. `model_validate` reports every error and warning in a model at once, as data, and takes the model inline (`model_source`) as well as by path, so a chat-hosted client that cannot place files on the server can still check its draft. `mgtt mcp serve --toolset all|diagnose|authoring` picks what is served; `authoring` reads no live system and writes nothing, and `about` reports the toolset. `make downstream` runs the authoring toolset against the real providers, validating the storefront model inline.
- **Every incident can become a regression test.** `mgtt incident end --emit-scenario` writes `scenarios/<incident-id>.yaml` next to the incident's model: the last observation of each recorded fact under `inject:` (or `unresolved:` when the probe came back `not_found`, `forbidden` or `transient`), and under `expect:` what the engine concludes from exactly those facts -- `root_cause` (or `none`), plus `eliminated` and `cannot_rule_out` when it reports them. The file is then replayed once through `mgtt simulate` and the command prints `wrote scenarios/<id>.yaml — passes; commit it to keep this diagnosis under test`. An existing file is never overwritten (a warning, exit 0), and an incident with no facts says so. Combines with `--suggest-scenarios`. Over MCP, `incident_end` takes `emit_scenario: true` and returns `scenario_path`, `scenario_yaml` and `scenario_passes`, or `scenario_warning` when nothing was written; it finds the model by the incident's `model_ref`. Both share one implementation (`simulate.RecordIncident`).
- **Scenario-guided diagnosis honours redundancy groups.** Enumerated chains carry a member's failure past its group only when the root reaches more members than the group can spare (one web down: no; the database under both: yes), and at diagnosis time a chain through a group that still holds is contradicted. `mgtt diagnose` reports a root covered by a holding group as redundancy degraded, not as the root cause. `model export` carries `group` and `need` on each grouped edge (consumers that ignore them see hard edges, as before). Scenarios can assert `expect.redundancy_degraded:`. New example: `examples/redundant-web/`, run by `make downstream`. Enumeration of models without groups is byte-identical to before.
- **Redundancy groups.** A `depends` entry with a list and `need: k` holds while at least k members are healthy: `- on: [web-a, web-b]` with `need: 1` models one-of-N (replicas, standby, two serving colors) instead of N hard dependencies. While a group is satisfied, a broken member is not the root cause and conclusions report it as redundancy degraded (`mgtt plan`, MCP `plan.redundancy_degraded`); a member of unknown health does not count toward k. The path engine and BFS honour groups.
- **`healthy:` says how it combines with the type's rules.** `healthy: { add: [...] }` keeps every type rule and requires these as well; `healthy: { replace: [...] }` replaces them, as a bare list does, but marks the drop deliberate so `mgtt model validate` does not warn. A bare list keeps working, means replace, and is the form that warns. `model build` keeps the form when it re-emits a component; export and every health verdict use the effective rules. The storefront example now validates with no warnings: its three idle-stage relaxations say `replace:`, and `rds` no longer overrides a type default it only restated.
- **Every conclusion names what it could not rule out.** A component whose facts could not be read (forbidden, transient) is never cleared, but conclusions did not say so: `mgtt diagnose` printed only a count ("Partial visibility: 2 forbidden"), and `mgtt plan` and MCP said nothing, so a root cause found elsewhere read as complete. Now `diagnose` and `plan` print `Cannot rule out: rds (available: forbidden, ...)`, MCP `plan` and `incident_snapshot` return `cannot_rule_out`, and with no root cause they say "none among the components that could be seen" rather than "all components healthy". Scenarios can assert it with `expect.cannot_rule_out:`.
- **Scenarios can record probes that produced no value.** A new `unresolved:` block takes component → fact → `forbidden` | `transient` | `not_found`, so CI can cover a refused probe or a deleted resource, which `inject:` values cannot express. A new `expect.not_eliminated:` asserts that components stay in play -- the claim `eliminated:`, a subset check, cannot make. The storefront example gains two such scenarios: RDS probes refused (rds must not be cleared) and RDS deleted (rds is the root cause). The refused one fails under the old fail-open rule only through `not_eliminated`: root cause and path are identical, rds is silently cleared.
- `mgtt model validate` warns when a component's rules compare against a variable that nothing sets, since such a rule can never be decided. `mgtt provider validate` accepts a bare word the provider declares under `variables:` as the value a comparison reads.
- **`mgtt model validate` names every type rule a `healthy:` override drops.** A component's `healthy:` replaces its type's rules rather than adding to them, so a rule not restated is gone -- the most common modelling mistake. Each dropped rule is now a warning (`healthy override drops type rule "available == true" ...`); rules compare with whitespace ignored. The storefront example dropped `available == true` from `rds` and `mq`, so a stopped database counted as healthy; both now restate it.
- The MCP server sends `instructions`: the diagnosis workflow and what a `forbidden` or `transient` probe result means, for clients that hand them to the model.
- **`make downstream`** builds this checkout and every repository that depends on it -- the six providers, compiled against this engine through a `replace`, and mgtt2writ with writ -- in one pinned Docker image, then runs the suites: provider unit tests and `provider validate`, the minishop verification contract, the storefront scenarios and probe-decision budget, the mgtt2writ pipeline and MCP tool-name portability. Images are pinned by digest and repositories by commit. Known breakages sit in `downstream/xfail`, each naming the step that fixes it; a listed suite that starts passing fails the build until its line goes.
- `examples/storefront/`: the blue/green storefront model and its five scenarios as files, extracted from the docs page.

### Changed

- **`scenarios.yaml` stores the failure graph, not every chain.** Every chain is a path through a small graph (components, their failure states, what each emits and what triggers it, which components a failure reaches), so the sidecar now stores that graph as `format: graph/v1` and mgtt expands it when loading. The storefront example's 12,755 chains went from 8.6 MB to 11 KB, so it now commits its sidecar and gets the drift check back instead of `scenarios: none`; a model change reviews as the edge it adds. Enumeration is now one algorithm over the graph, and its output is byte-identical to before (storefront and redundant-web checked); diagnosis on the graph file matches the chain file run for run. mgtt still reads the chain-list format, and rejects a format it does not know.
- **Scenario-guided diagnosis decides a probe about 800x faster on large models.** Occam recounted each candidate's cross-elimination score inside the sort comparator, O(n² log n) in live scenarios; one decision on the 20-component storefront (12,755 scenarios) took about 30 s. The score is now computed in one pass over the live set, O(n·L): about 37 ms per decision there. Decisions are unchanged, checked against the old implementation on 300 randomised fact stores.

### Fixed

- **Provider types agree with themselves.** The consistency check also reports rules that fail where no state matches (a failure with no state to name), and no longer invents values for string facts, which are closed enums. With it, the kubernetes (21 types) and aws (9 types) providers were reconciled so healthy rules and states agree; aws gains failure states for breached thresholds (rds `saturated`, mq `backlogged` / `no_consumers`, elasticache `cold`, ...). The storefront acknowledges its deliberate staging relaxations against the new states (`cold`, `no_consumers`, opensearch's rollout states) with `healthy_diverges_from`, its scenarios inject the operator's `restart_count`, and its sidecar is regenerated (12,755 → 15,982 scenarios).
- **`mgtt model build` keeps hand-written components.** Anything discovery did not return -- a business process, an external SaaS, hand-written wiring, where the modelling judgement lives -- was proposed for deletion on every rebuild, and the build refused to proceed until it was tombstoned. Components now carry `source:`: build writes `source: discovered` on what it found, anything else is authored and kept on every rebuild, listed as kept, with any dependency on a component the model no longer has flagged as dangling. Only discovered components that disappear go through the deletion gate. In a model built before this change no component has the marker, so build keeps everything and lists what it kept rather than deleting anything; the next build marks what it discovers.
- **Simulate and diagnose name the same root cause, by one rule.** Fuzzing both engines over 5,000 fact sets mixing healthy, broken and unreadable facts found them disagreeing on 28%, each wrong in its own way: the path engine ranked candidates by shortest distance from the entry, so it blamed a frontend over the api it depends on; the live strategy broke ties alphabetically, so it blamed a frontend over a database that could have broken it through an unread api. Both now call `strategy.RootCause`: a component seen broken that no other broken component could have caused -- through dependencies not proven healthy, over active edges, not through a group that holds -- and of several, the most upstream. The fuzz test now passes with no divergence.
- Parsing several scenarios from one source no longer turns a stray leading or trailing `---` into an extra, empty scenario that expects nothing.
- **`mgtt diagnose` records what it probes in the active incident.** It appended probe results and operator answers to the incident's store but never saved it, so after `mgtt incident start; mgtt diagnose; mgtt incident end` the state file held `facts: {}` and `--suggest-scenarios` saw nothing. It now saves after every probe and answer, as `mgtt plan` already did; with no incident it still keeps facts in memory only.
- `mgtt model build` no longer reports a provider whose discovery failed (a timeout, a backend error, unparseable output) as having "no Discover() support". That line is now kept for providers that have no discovery -- no binary, or an SDK refusal -- and the rest print `discover failed (skipped): <error>`.
- **The blue/green storefront no longer blames the idle color.** `acme-shop-svc` hard-depended on both colors, so a crash-looping idle green was reported as the root cause of an outage it was not causing (G1). Its edges now hold only while the Service's selector names their color (`while: selector_value == blue`, with mgtt-provider-kubernetes' new `service.selector_value` fact). Two scenarios pin it in both directions: idle green down gives no root cause, live green down names green. Storefront: 9/9.
- The path engine could not name the entry point as the root cause: it walked paths from the entry but never gave the entry a path of its own, so a broken entry with everything below it healthy produced no root cause while diagnose named the entry. A broken entry is now a candidate, outranked by anything deeper seen broken.
- **A component that cannot be found is a finding, not an all-clear.** When every probe of a component returned `not_found`, the scenario engine contradicted all of its failure states, and the path engine could not name it; a deleted database could never be the root cause, and its casualty was blamed instead. Now absence contradicts only the component's healthy default state, its failure states stay live, and it counts as unhealthy, so it can be named. `mgtt diagnose` says the root cause was not found rather than implying it was seen in a failure state.
- **Rules that compare against a per-component threshold are decided.** docker, quickwit and tempo write rules like `restart_count <= max_restart_count`, with the threshold set per component under `vars:`. The evaluator looked bare words up only as facts, so those rules never resolved: before the unknown-facts fix they were silently skipped, after it the component stayed undecided. A bare word that names no fact now resolves as a variable -- the component's `vars:`, then `meta.vars:`, then the provider's declared default. A fact of the same name still wins; an unset or non-numeric variable leaves the rule unresolved, never healthy.
- **The root cause is a component seen broken, never the deepest one nobody has probed.** The path engine named the tail of the longest surviving path, and a path survived when its tail had no facts but anything above it was sick. On the storefront example that blamed `ssm-app-config` -- never probed -- for both an nginx crash loop and an RDS outage. Now a path whose tail is unobserved still keeps the engine probing inward but names nothing; the root cause is the deepest tail observed unhealthy. A path through a component proven healthy is refuted, since a failure cannot pass through it, unless its own tail is seen broken, which stays a finding. All five storefront scenarios pass.
- **The MCP `probe` tool runs probes the way the CLI does.** It used to run every probe as a shell command, skipping provider runners. A fact with no `probe.cmd` -- every fact of a runner-backed provider such as aws -- came back `operator_prompt_required`, so an agent could never probe aws at all, and kubernetes probes bypassed the provider binary the CLI uses. `mgtt plan`, `mgtt diagnose` and MCP now share one dispatcher (`probe/dispatch`): the same runner routing, `MGTT_FIXTURES` support, and the same recording of outcomes. A probe the backend refuses or that times out is recorded as an unknown fact everywhere; over MCP it now returns `forbidden` or `transient` instead of an unrecorded `error`, and `mgtt plan` keeps going instead of stopping. `operator_prompt_required` now means what it says: no command and no runner.
- A provider gets a runner only when its binary exists or its manifest declares an entrypoint. A types-only provider used to be routed to a `bin/mgtt-provider-<name>` that was never built; its `probe.cmd` now runs.
- **Facts that could not be read no longer clear a component.** The path engine eliminated any component with a recorded fact unless a rule came out definitively false, so a component whose probes all returned 403 or timed out was reported healthy and its casualty blamed instead. Health is now three-valued (`strategy.ComponentVerdict`): a component is eliminated only when every effective `healthy:` rule resolves true. Forbidden, transient and missing facts leave it Unknown, and Unknown is never eliminated.
- `mgtt provider validate` no longer requires `probe.cmd` on providers whose probes go to a runner binary, which never reads it. aws failed 39 checks for this.
- `mgtt provider validate` reads a bare word compared against a string fact (`phase == Bound`) as the literal the evaluator compares it as, not as a reference to an undeclared fact. kubernetes failed 18 checks for this. Against a numeric fact a bare word is still a fact reference, so a typo there is still caught.

## [0.3.0] — 2026-09-12

### Breaking (SDK)

- `sdk/provider.Registry.Register` and `RegisterDiscover` now panic on a
  second call for the same type (or on a second discovery function).
  Previously they silently replaced the prior registration — a
  double-register is a programmer bug 100% of the time, and silent
  replacement made it manifest as mysterious probe results. Provider
  authors calling `Register` idempotently (e.g. on SIGHUP reload) must
  now guard with a sentinel flag or build a fresh `*Registry`.
- `sdk/provider.Registry.Types()` and `Facts()` removed. The docstring
  claimed they were consumed by validate tooling; no such tooling
  exists. Code that depended on them should enumerate registered types
  at the author's own site.

### Added

- **`mgtt model export --json`** emits the resolved model — provider types merged, component overrides applied — as a versioned JSON document. It names no other tool; the document is equally useful to a visualiser or an editor.

  Checking that model over every reachable failure configuration is a pipeline rather than an mgtt subcommand: `mgtt model export --json | mgtt2writ | writ check --stdin`, or `mgtt-contradict-check` for the one-command form. The translator lives in [mgt-tool/mgtt2writ](https://github.com/mgt-tool/mgtt2writ) and the checker is [writ](https://github.com/writ-lang/writ).

  Where `simulate` tests the scenarios you wrote, this reports over all of them: whether a component can be healthy and not in its default active state (the `simulate`/`diagnose` divergence class, now a law with a route to the nearest violation), which moves can break that law, and which configurations are dead ends. Exit status is writ's — 0 clean, 1 a finding — so it sits beside `mgtt simulate --all` in CI.

  An earlier draft of this shipped a `mgtt verify` subcommand that shelled out to writ. It was removed before release: mgtt should not carry a verb that depends on a third-party binary. See [ADR 0001](docs/decisions/0001-where-the-writ-bridge-lives.md).
- **`mgtt mcp serve` — MCP service for LLM agents.** Exposes mgtt's engine as an MCP (Model Context Protocol) server so agents in CI pipelines or IDE coding tools can drive the full diagnosis loop via typed tool calls. Phase 1 ships ten tools (`about`, `incident.start`, `incident.end`, `fact.add`, `facts.list`, `plan`, `probe`, `scenarios.list`, `scenarios.alive`, `incident.snapshot`) over two transports: **stdio** (default, zero-config, for IDE subprocess spawn) and **streamable HTTP** (`--http --listen :8080`, bearer-token auth required via `--token-env`). Safety flags default to capability: `--readonly-only` rejects non-read-only providers; `--on-write={pause,run,fail}` polices write probes; `--max-execute-per-incident` caps executions per incident; all blocked probes still surface the rendered command. Multiple incidents can run concurrently in one `$MGTT_HOME` (MCP path does not touch the CLI's `.mgtt-current` pointer); state format is identical to the CLI, so a human running `mgtt status <id>` sees the same incident. See `docs/reference/mcp.md` for transports, safety flags, and the Docker-sidecar recipe.

- **Scenario-based troubleshooting.** New `internal/scenarios` + `internal/engine/strategy` packages + `mgtt diagnose` autopilot. Models declare `state.triggered_by: [label]` and providers declare `failure_modes.{state}.can_cause: [label]` to describe how failures propagate; an offline enumerator walks that graph into a committed `scenarios.yaml` per model plus a workspace `scenarios.index.yaml`. Two strategies (`occam` for scenarios-backed runs, `bfs` for generic walk) plug into a `Strategy` interface with auto-selection. `mgtt diagnose` runs a probe-selection loop with `--suspect`, `--readonly-only`, `--max-probes`, `--deadline`, `--on-write`. `mgtt simulate --from-scenarios` + `--fuzz` exercise the same scenarios.yaml as test input. `mgtt incident end --suggest-scenarios` learns new chains from real incidents.
- **Generic fallback provider.** When no installed provider owns a component's type, `mgtt` resolves it to an embedded `generic.component` type and prompts the operator interactively at diagnose time. Validate emits one `INFO` line per fallback so authors see it. Opt out with `meta.strict_types: true` (rejects generic resolution as a validation error). The `"generic"` provider name is reserved: `mgtt provider install` refuses third-party manifests claiming it, and registry-load paths refuse to boot if an on-disk provider shadows the built-in.
- **`mgtt model validate --check-scenarios`.** Fast CI lane: runs ONLY the `scenarios.yaml` drift check, skipping structural / type / dep-ref validation. For repos that already pass full validate and only want to catch stale sidecars.
- **`meta.scenarios: none` opt-out.** Placeholder models (empty or WIP) can declare `meta.scenarios: none` to skip the `scenarios.yaml` drift check and `--write-scenarios` regeneration. Keeps the sidecar invariant without forcing an empty file.
- **`state.triggered_by` validation.** New pass 5 emits a warning when a `triggered_by` label has no producer (no `can_cause` mentions it across the registry + the model's own `failure_modes` overrides). Catches the typo class where a state becomes unreachable.
- **Absent-component elimination.** `internal/facts.FactStatus` + `FactStatusNotFound` + `Store.IsAbsent` plumb authoritative "the probe ran but the resource is missing" into the live-set filter. Scenarios requiring a non-default state on an absent component are dropped; the default-active state stays live.

- **Provider capabilities.** Providers declare semantic needs at the top level of `manifest.yaml` (`needs: [kubectl, network]`); mgtt's image runner expands each label into the matching `docker run` bind mounts and env forwards at probe time (git installs inherit them from the operator's shell). Built-in vocabulary covers `network`, `kubectl`, `aws`, `docker`, `terraform`, `gcloud`, `azure`. Operators override or extend via `$MGTT_HOME/capabilities.yaml` (+ `capabilities.d/*.yaml` shards) or `MGTT_IMAGE_CAP_<NAME>` env vars; `MGTT_IMAGE_CAPS_DENY=docker,aws` refuses capabilities regardless of declaration. Install-time prints declared caps for audit. `mgtt provider ls` shows a caps column per installed provider. Validation rejects unknown caps and refuses `needs` on shell-fallback providers. See `docs/reference/image-capabilities.md`.
- **A release pipeline, and three ways to pin a version.** Pushing `vX.Y.Z`
  now publishes a GitHub release (`mgtt-<os>-<arch>` for linux/darwin ×
  amd64/arm64, with `SHA256SUMS`, notes quoted from this file), the image at
  `ghcr.io/mgt-tool/mgtt:X.Y.Z` (and `X.Y`, `X`, `latest`; linux/amd64 and
  linux/arm64), and verifies all three from the outside — `go install @tag`,
  `go get sdk/provider@tag`, `docker pull`, and `install.sh` — each answering
  `mgtt version` with the tag. `scripts/release-check.sh` refuses a tag whose
  tree disagrees with `VERSION` or whose changelog still has unreleased
  entries; `make tag` runs it first. (`.github/workflows/release.yaml`)
- `install.sh` takes `MGTT_VERSION=vX.Y.Z` and `INSTALL_DIR`, verifies the
  binary against the release's `SHA256SUMS`, and falls back to `go install`
  at the same version rather than a clone of main.
- `make dist` cross-compiles every release platform; CI runs it on every push.

### Breaking

- Schema: capabilities moved from `image.needs: [...]` to a top-level `needs: [...]` block in `manifest.yaml`. The underlying requirement ("this provider needs kubectl") is a property of the provider, not of the image-install runtime that happens to translate it into flags. Pre-1.0, ripped without an alias — providers must update their YAML.
- Schema: `network` split out of the capability vocabulary into its own top-level field. Previously `needs: [kubectl, network]` conflated two categories — host-resource grants and docker-run isolation mode. Now: `needs: [kubectl]` (tools/creds only) plus `network: host` (or `bridge`/`none`). `mgtt provider validate` rejects `network` as a cap name and validates the new field against the `bridge|host|none` set. All six registry providers updated.
- Schema: `auth:` block ripped. `auth.strategy` / `auth.reads_from` / `auth.access.probes` were freeform prose that mgtt never parsed or enforced — pretending they were structured data hurt more than it helped. `auth.access.writes` had a provider-invented string vocabulary (`"none"`, `"state-refresh-on-plan"`, etc.) that surfaced only as a validation WARN. Replaced with two fields at provider-yaml top level: `read_only: true` (default) or `false` plus a required `writes_note:` prose when `false`. `mgtt provider validate` now enforces the pair; `mgtt provider install` prints the note so operators consent knowingly. Credentials the provider reads (which env vars, which config paths) move out of manifest.yaml and into each provider's README, where they can be narrative and accurate.

### Fixed

- **`internal/expr` tokenizes double-quoted string literals.** Previously the tokenizer rejected `"` outright, so `health_status != "unhealthy"` failed to parse. Now `"..."` is a first-class value token: quotes are stripped and the content is taken as a string without bool/int inference (so `"42"` stays the string `42`, not the int). Unquoted barewords remain accepted for back-compat. Unblocks the docker provider's `container` type healthy expression noted under 0.2.0 Known Issues.
- **`mgtt provider install --image` now works with distroless and scratch-based provider images.** `ExtractManifest` previously shelled out to `docker run --rm --entrypoint cat <image> /manifest.yaml`, which required the provider image to ship a `cat` binary on `PATH`. Switched to `docker create` + `docker cp <cid>:/manifest.yaml -` + `docker rm`, decoding the resulting tar stream in-process. Nothing inside the container is executed, so any base image works. No docs, flags, or on-disk state change.
- **`mgtt provider install --image` now extracts `/types/` for multi-file providers.** The installer previously copied only `/manifest.yaml` out of the image. Providers with a `types/<name>.yaml` layout (kubernetes, tempo, quickwit, terraform) would land in `~/.mgtt/providers/<name>/` with no type definitions — `mgtt provider inspect` would report zero types and planning against the provider would silently skip its components. `installFromImage` now calls the new `DockerCmd.ExtractTypes`, which `docker cp`s the `/types/` directory out of the image and writes each `.yaml` entry into `destDir/types/`. Absence of `/types/` is not an error (inline-types providers still work).

### Changed

- `mgtt version` prints `vX.Y.Z` — the spelling the Go proxy and git tags
  use — from every channel alike. Builds from `make`, the image and the
  release assets used to print `X.Y.Z` without the `v`; `go install` builds
  already printed it with.
- The `latest` image tag is now written only by a release. Pushes to main
  publish `edge` and `sha-<commit>` instead, so `latest` never runs ahead of
  the newest release.
- GitHub Actions are pinned by commit, and the Go toolchain by `go.mod`.
- `make docker` builds with `docker build`; the compose file it referred to
  did not exist.

## [0.2.0] — 2026-04-18

### Changed (breaking)

- **`manifest.yaml` schema rewritten.** v0.x manifests are rejected. Three top-level blocks: `meta` (identity), `runtime` (needs + network_mode + entrypoint + backends), `install` (source + image subblocks declare which methods the provider offers). `hooks:` retired; `meta.command` moved to `runtime.entrypoint`; install-method declaration is now canonical in the manifest instead of inferred from hook presence + registry state. See [`docs/reference/manifest.md`](docs/reference/manifest.md).

- **All in-tree providers updated to v1.0.** aws 1.0.0, kubernetes 3.0.0, docker 1.0.0, tempo 1.0.0, terraform 1.0.0, quickwit 1.0.0. Older provider versions won't install against mgtt 0.2.0.

- `mgtt provider install --image` rejects providers without an `install.image` block; `mgtt provider install` (source) rejects providers without `install.source`. No more deep-in-build failures.

### Known issues

- ~~`mgtt-provider-docker`'s `container` type declares a `healthy:` expression comparing a string literal (`health_status != "unhealthy"`). The current `internal/expr` parser does not tokenize string literals, so loading that specific type fails at runtime. The rest of the docker provider's manifest is v1.0-valid and the migration commit is on main; a follow-up will extend the expression tokenizer to accept double-quoted string literals.~~ **Resolved in [Unreleased]** — see _Fixed: `internal/expr` tokenizes double-quoted string literals_.

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
