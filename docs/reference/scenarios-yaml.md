# `scenarios.yaml`

The sidecar that `mgtt model validate --write-scenarios` drops next to a model. It holds the model's **failure graph**: per component, the facts it is observed through, its failure states with what they emit and what triggers them, and which components its failure reaches. Every plausible failure chain is a path through that graph, and `mgtt diagnose` uses those chains as its search space at incident time.

The graph is small even when the chains are not. The blue/green storefront example has 12,755 chains, which took 8.6 MB as a list and take 11 KB as a graph. A model change shows in review as the state or edge it adds, not as thousands of renumbered chains.

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
# GENERATED — rebuild via `mgtt model validate --write-scenarios`. Do not hand-edit.
source_hash: sha256:9252fcac729b3abc9bdb16231715d1c1401e8600a3ebe970c21c380bc488ba53
format: graph/v1
scenario_count: 4
components:
  api:
    observes: [ready_replicas, restart_count]
    states:
      - name: crashed
        emits: [upstream_failure]
    dependents:
      - to: edge
  db:
    observes: [available]
    states:
      - name: stopped
        emits: [upstream_failure]
    dependents:
      - to: api
  edge:
    observes: [upstream_count]
    states:
      - name: draining
        triggered_by: [upstream_failure]
```

## Fields

| Field | Description |
|-------|-------------|
| `source_hash` | sha256 of the model + referenced type YAMLs. Used by `mgtt model validate` to detect drift. |
| `format` | `graph/v1`. A file without it is the older list of chains, which mgtt still reads. |
| `scenario_count` | How many chains the graph expands to: for readers and diffs. mgtt recomputes it. |
| `components.<name>.observes` | The type's facts: what a chain ending at this component is observed through. |
| `components.<name>.states[]` | The type's failure states (not its default state), in type order: `emits` is its `can_cause`, `triggered_by` the labels that can put it in that state (none: any). |
| `components.<name>.dependents[]` | Components that depend on this one. `group: true` with `roots:` marks a [redundancy group](model-schema.md#redundancy-groups) edge, which carries only the listed roots' failures. |
| `components.<name>.unresolved` | The component's type did not resolve: no chain runs through it. |

A chain starts at a component's failure state and steps to a dependent's failure state whenever one of its `emits` labels is in that state's `triggered_by`. It ends at any component that has `observes`. Its IDs (`s-0001`, ...) number the expanded, sorted chains; they are what `scenarios_list` and `diagnose` report.

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

Placeholder models that don't yet describe failure modes can opt out. Size is no longer a reason to opt out; the graph stays small.

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
