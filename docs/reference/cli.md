# CLI Reference

```
mgtt init                              Scaffold system.model.yaml
mgtt model validate [path]             Validate model

mgtt provider install <name|path|url>  Install a provider
mgtt provider ls                       List installed providers
mgtt provider inspect <name> [type]    Inspect provider types

mgtt simulate --all                    Run all scenarios
mgtt simulate --scenario <file>        Run one scenario

mgtt incident start [--id ID]          Start incident session
mgtt incident end                      Close incident

mgtt plan [--component NAME]           Guided troubleshooting
mgtt fact add <c> <k> <v> [--note ..]  Add observation

mgtt ls                                List components
mgtt ls facts [component]              List facts
mgtt status                            Health summary

mgtt stdlib ls                         List primitive types
mgtt stdlib inspect <type>             Type definition
```
