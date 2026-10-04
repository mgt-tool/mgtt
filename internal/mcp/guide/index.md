# mgtt: writing a model

mgtt reasons about a system from one YAML model: its components, their types (from installed providers), how they depend on each other, and what "healthy" means for each. The same model drives two things: `mgtt simulate`, which runs scenarios in CI, and `mgtt diagnose`, which runs live probes during an incident.

## The authoring loop

1. `types_list`, then `types_describe <type>` for each type you use. Use only the fact names it lists. Read a type's `healthy` rules before overriding them.
2. Draft the model and send it to `model_validate` as `model_source`. Fix every error and read every warning. A warning about a dropped type rule or an unset variable is usually a bug.
3. Write scenarios (topic `scenarios.authoring`) and run them with `scenario_simulate` until they pass.
4. The model reaches the repository as a reviewed change. No tool writes it.

These tools read installed providers and what you send, never a live system.

## Topics

- `model.health`: `healthy:` overrides, `replace:` and `add:`, thresholds in variables
- `model.dependencies`: plain dependencies, redundancy groups (`need:`), guards (`while:`)
- `model.patterns`: patterns from real models: symptom layers, resource names, namespaces
- `scenarios.authoring`: what to test, the scenario archetypes, how expectations match
