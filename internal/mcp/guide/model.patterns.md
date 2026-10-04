# Patterns from real models

- **Model the user-visible symptom.** Business processes (scheduled jobs run on time, the queue drains) can be components with facts an operator can observe. A chain needs a symptom to end at, and these are the symptoms people report.
- **Readable keys, real resource names.** Use short keys (`rds`, `redis`) and put the cloud identifier in `resource:` (`acme-shop-{env}-rds`). Keys are what people read in a diagnosis; resources are what probes query.
- **Variables for what varies.** `meta.vars:` holds model-wide values such as namespace, region and env. A component's `vars:` overrides them for that component only, for example an operator in its own namespace.
- **Model the operator, not what it produces.** An operator's health stands for the secret or config it renders, and probing the secret often needs permissions a diagnose role should not have.
- **Choose probes the diagnose role can run.** A probe that is always refused (403) leaves its component undecided on every incident. Prefer the fact a read-only role can see.
- **Leave out what does not break the service.** Observability stacks and cluster operators that do not serve traffic add chains without adding answers.
