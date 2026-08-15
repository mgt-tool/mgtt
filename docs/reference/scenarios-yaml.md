# `scenarios.yaml`

The sidecar that `mgtt model validate --write-scenarios` drops next to a model. It enumerates every plausible failure chain the model can produce — one `id` per chain — and `mgtt diagnose` uses it as the search space at incident time.

Not to be confused with hand-authored [simulate scenarios](scenario-schema.md) — those stay under `scenarios/` and use `inject:` / `expect:`. This file is generated.

## On this page

- [Anatomy](#anatomy)
- [Fields](#fields)
- [Regenerate](#regenerate)
- [Drift detection](#drift-detection)
- [Opt out](#opt-out)
- [How diagnose uses it](#how-diagnose-uses-it)
- [Learning new chains from incidents](#learning-new-chains-from-incidents)

---

## Anatomy

```yaml
# GENERATED — rebuild via `mgtt validate --write-scenarios`. Do not hand-edit.
source_hash: sha256:9252fcac729b3abc9bdb16231715d1c1401e8600a3ebe970c21c380bc488ba53
scenarios:
  - id: s-0001
    root:
      component: edge
      state: stopped
    chain:
      - component: edge
        state: stopped
        observes:
          - operator_says_healthy
  - id: s-0002
    root:
      component: api
      state: stopped
    chain:
      - component: api
        state: stopped
        emits_on_edge: upstream_failure
      - component: edge
        state: stopped
        observes:
          - operator_says_healthy
```

## Fields

| Field | Description |
|-------|-------------|
| `source_hash` | sha256 of the model + referenced type YAMLs. Used by `mgtt model validate` to detect drift. |
| `scenarios[].id` | Stable identifier (`s-0001`, `s-0002`, …). Survives regeneration as long as the chain does. |
| `scenarios[].root` | `{component, state}` — the root-cause node of the chain. |
| `scenarios[].chain[].component` | Component visited on the way from root to symptom. |
| `scenarios[].chain[].state` | The failure state at that component. |
| `scenarios[].chain[].emits_on_edge` | The `can_cause` label the previous step emitted to trigger this one. |
| `scenarios[].chain[].observes` | The facts this terminal step is observable through — what operators see at 3am. |

## Regenerate

```bash
mgtt model validate --write-scenarios              # auto-detect model
mgtt model validate --write-scenarios path/to.yaml # specific model
```

Running without a path regenerates every model in the workspace and writes a summary `scenarios.index.yaml` at the workspace root.

## Drift detection

Every `mgtt model validate` invocation checks that the sidecar's `source_hash` still matches the model + types on disk. A mismatch fails the command with an actionable message:

```
$ mgtt model validate
Error: scenarios.yaml is stale: source_hash=sha256:925... but current content hashes to sha256:7d8...
       Run `mgtt model validate --write-scenarios` and commit.
```

Fast CI lane — only the drift check, skip everything else:

```bash
mgtt model validate --check-scenarios
```

## Opt out

Placeholder models that don't yet describe failure modes can opt out:

```yaml
meta:
  scenarios: none
```

Drift check is skipped; `--write-scenarios` regeneration is skipped.

## How `diagnose` uses it

`mgtt diagnose` reads `scenarios.yaml` as its candidate set. The live-set filter eliminates chains whose intermediate states contradict observed facts (or whose components are absent), and the `occam` strategy ranks the rest by chain length, surviving-scenario ratio, and optional `--suspect` hints.

Absent `scenarios.yaml`, diagnose auto-switches to `bfs` — walks the dependency graph from the outermost symptom inward, one probe at a time.

## Learning new chains from incidents

If a real incident doesn't match any enumerated chain, propose an extension:

```bash
mgtt incident end --suggest-scenarios
```

Writes a patch file alongside the incident. Review, merge into `scenarios.yaml` (or adjust the model so regeneration covers it), commit.
