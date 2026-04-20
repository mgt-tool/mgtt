# CLI Reference

Every `mgtt` subcommand. Flags default to safe, read-only behaviour unless stated otherwise.

## Model

```
mgtt init                              Scaffold system.model.yaml in the cwd
mgtt model validate [path]             Structural + type + dep-ref checks; drift-check scenarios.yaml
  --write-scenarios                    Regenerate scenarios.yaml next to the model (or a
                                       scenarios.index.yaml across a workspace when no path)
  --check-scenarios                    Run only the scenarios.yaml drift check (fast CI lane)
```

### `mgtt model build`

Generates `system.model.yaml` from installed providers' discovery output. Commit the result alongside your Helm charts / Terraform.

```bash
mgtt model build
mgtt model build --allow-deletes
mgtt model build --tombstone=legacy-api,air-gapped-db
mgtt model build --output custom-path.yaml
```

**Flags**

| Flag | Default | Purpose |
|------|---------|---------|
| `--mgtt-home` | `$MGTT_HOME` / `~/.mgtt` | Where to look for installed providers |
| `--output` | `system.model.yaml` | Destination file |
| `--allow-deletes` | `false` | Accept removal of components no longer returned by discovery |
| `--tombstone` | `[]` | Components to preserve across rebuilds (air-gapped, partial discovery) |

**Safety**

By default, if the build would remove components from the existing committed model, the command refuses and prints the removal set. This protects against partial-discovery failures (kubectl timeout, IAM expiry mid-enumeration) silently nuking half the model.

```
$ mgtt model build
  kubernetes provider    → 11 components, 7 dependencies

Model drift detected (vs committed system.model.yaml):
  -  legacy-api
  -  old-rds

Refusing to remove components without explicit consent. Options:
  mgtt model build --allow-deletes
  mgtt model build --tombstone=legacy-api,old-rds
```

**Determinism**

`mgtt model build` run twice against the same infrastructure produces byte-identical YAML. Sorted keys, sorted dependency lists, no timestamps. A `git diff` after a no-op run prints zero bytes.

## Providers

```
mgtt provider install <name|path|url>  Install from registry / git / local / image ref
  --image <ref>                        Force image install even if source block exists
mgtt provider ls                       List installed providers
mgtt provider inspect <name> [type]    Show provider manifest + types + facts + states
mgtt provider uninstall <name>         Run the provider's cleanup hook + remove the directory
```

## Simulation

```
mgtt simulate                          Run failure scenarios against a model (design-time)
  --all                                Run every YAML under scenarios/
  --scenario <file>                    Run a single hand-authored scenario
  --from-scenarios                     Iterate enumerated scenarios.yaml as test cases
                                       (asserts Occam identifies each root)
  --fuzz <N>                           Random scenario + random truncation, N iterations
  --fuzz-seed <int>                    Deterministic seed for --fuzz
  --scenarios-dir <dir>                Override directory for hand-authored scenarios
  --model <path>                       Override model path
```

## Troubleshooting

```
mgtt plan [--component NAME]           Interactive guided troubleshooting (press Y per probe)

mgtt diagnose                          Autopilot — runs probes until root cause or budget
  --model <path>                       Override model path
  --suspect api,db.down                Soft prior: components (or component.state) to prefer
  --readonly-only                      (default true) refuse probes that aren't read_only
  --max-probes <N>                     Probe budget (default 20)
  --deadline <duration>                Wall-clock deadline (default 5m)
  --on-write pause|run|fail            Behaviour when the next probe would write (default pause)

mgtt status                            One-line health summary from collected facts
mgtt ls                                List components (or `mgtt ls facts [component]`)
```

## Incident lifecycle

```
mgtt incident start [--id ID]          Start session; opens a fresh state file
mgtt incident end                      Close session; render the final report
  --suggest-scenarios                  Emit a scenarios patch proposing new chains learned
                                       from this incident (for review + commit)
mgtt fact add <c> <k> <v>              Record an operator observation
  --note "..."                         Free-text provenance
```

## Stdlib introspection

```
mgtt stdlib ls                         List primitive types mgtt knows about
mgtt stdlib inspect <type>             Full definition for a stdlib type
mgtt version                           Print mgtt version
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Validation / diagnose / simulate failure |
| `2` | Usage error (bad flags, missing file) |
| `3` | Panic — recovered at top level; see stderr for bug-report details |
