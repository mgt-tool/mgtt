# Model Authoring

Two ways to get a `system.model.yaml`:

- **Hand-author** — write it yourself, commit it. What mgtt has always supported.
- **Generate from discovery** — `mgtt model build` invokes installed providers' `discover` subcommand and writes the YAML for you.

Both produce the same file format. You can start with one and switch, or mix them. `mgtt model build` writes exactly what current discovery reports. Components that prior builds discovered but aren't returned this run are treated as deletions — either explicit (`--allow-deletes`) or preserved (`--tombstone=<name>`). Hand-authored components that were never discovered by any provider are currently lost on rebuild; see the _Hand-authored parts_ section below for the workaround.

## The flow

```
1. mgtt provider install kubernetes aws   # providers know the topology
2. mgtt model build                        # writes system.model.yaml
3. git diff system.model.yaml              # review what changed
4. mgtt simulate --all                     # scenarios still pass?
5. git commit system.model.yaml            # ship it
```

Step 4 is the safety net: if the topology changed in a way that breaks your authored scenarios, simulate catches it before the commit.

## When discovery fails safe

If a provider's `discover` subcommand exits non-zero (older provider, backend API down, IAM temporarily expired), `model build` logs a warning and skips that provider. Its components aren't removed from the existing model — the deletion gate refuses.

```
$ mgtt model build
  kubernetes provider → 11 components, 7 dependencies
  aws provider        → no Discover() support (skipped): backend API timeout

Model drift detected: 3 components removed.
Refusing to remove without --allow-deletes. Investigate first.
```

A partial discovery failure LOOKS like an intentional decommissioning. The gate forces you to look before accepting.

## What about hand-authored parts?

`mgtt model build` only writes components returned by at least one provider's `discover`. Components you hand-add to `system.model.yaml` (external SaaS dependencies, legacy systems, cross-provider wiring) will be flagged for removal on the next `model build` unless you pass `--tombstone=<name1>,<name2>` to carry them forward, or `--allow-deletes` to accept the removal. A future plan adds catalog sources (Backstage, OpsLevel) that cover more of the graph automatically; what's left after that is the irreducible hand-authored surface — which the tombstone workflow handles today.

## See also

- [CLI: mgtt model build](../reference/cli.md#mgtt-model-build)
- [Model Schema](../reference/model-schema.md) — the file format
- [Simulation](simulation.md) — how scenarios guard the generated model
