# Scenarios: pinning what the model must conclude

A scenario injects facts, optionally records probes that failed, and states the conclusion the model must reach. Run scenarios with `scenario_simulate`, or `mgtt simulate --all` in CI.

Start from `scenario_suggest` (`mgtt simulate --suggest`): one draft with everything healthy and one per root and root state, with facts that put the failure's chain in its states and everything out of its reach healthy. A draft records what the engine concludes today, so it passes as written: read each one, and treat `review` as a question about the model. `unshowable` names failure states the healthy rules call healthy, which no scenario can show.

```yaml
name: rds unavailable
entry: edge                       # where diagnosis starts; default: the model's entry point
inject:
  rds:    { available: false, connection_count: 0 }
  api:    { ready_replicas: 0, desired_replicas: 3, condition_available: false, restart_count: 14 }
unresolved:                       # probes that ran and produced no value
  redis:  { available: forbidden }    # forbidden | transient | not_found
expect:
  root_cause: rds                 # strict; `none` when nothing is broken
  path: [edge, api, rds]          # ordered subsequence of the actual path
  eliminated: [mq, opensearch]    # subset of what was ruled out
  not_eliminated: [redis]         # must stay in play
  cannot_rule_out: [redis]        # the conclusion must say it could not see it
  redundancy_degraded: [web-b]    # broken, but its group still holds
```

`eliminated` passes with extras, so it cannot catch a component cleared that should not have been. Use `not_eliminated` for that.

A failure the model's entry point can't reach, such as a queue consumer behind a background job, is never named from there: set `entry` to the component where it is seen.

## The archetypes: write at least these

1. **All healthy:** every component healthy, `root_cause: none`. Catches a rule that trips on a healthy system.
2. **A failure with a healthy alternative:** the failure is named, and the healthy sibling is eliminated.
3. **A cascade:** one data-layer failure that breaks many components. The root is the data layer, not a casualty.
4. **An observability trap:** symptoms that point at the wrong component. The scenario pins the right one.
5. **A deep chain cut short:** a failure deep in the graph, observed only near the top.
6. **A refused probe:** `unresolved: {X: {fact: forbidden}}` on the true root. Expect `not_eliminated: [X]` and `cannot_rule_out: [X]`. Unknown is never healthy.
7. **A missing resource:** every fact of X `not_found`, so `root_cause: X`. A component the model expects and the system lacks is the finding.
8. **Redundancy:** one group member down gives `root_cause: none` and `redundancy_degraded: [it]`. For active/passive pairs, test the idle member down (no root cause) and the live member down (it is the root cause).

Inject every fact a type's healthy rules and states read. A missing one leaves the component undecided, which is rarely what the scenario means.
