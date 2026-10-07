// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"errors"
	"log"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// FilterLive returns the subset of scenarios consistent with the fact
// store. A scenario is live iff every step (component, state) is either:
//   - Unverified — no facts for component collected yet.
//   - Confirmed — facts collected AND state.When evaluates to true.
//
// A scenario is eliminated iff any step is contradicted (state.When
// evaluates to false under collected facts). Undefined predicates
// (missing facts) leave the scenario live.
func FilterLive(scs []scenarios.Scenario, store *facts.Store, m *model.Model, reg *providersupport.Registry) []scenarios.Scenario {
	var live []scenarios.Scenario
	for _, s := range scs {
		if isLive(s, store, m, reg) {
			live = append(live, s)
		}
	}
	return live
}

// Liveness is FilterLive's test over the graph, for callers outside a
// decision: what the facts in store leave live.
func Liveness(m *model.Model, reg *providersupport.Registry, store *facts.Store) scenarios.Liveness {
	return graphLiveness(Input{Model: m, Registry: reg, Store: store})
}

func isLive(s scenarios.Scenario, store *facts.Store, m *model.Model, reg *providersupport.Registry) bool {
	for i, step := range s.Chain {
		if !stepConsistent(step, store, m, reg) {
			return false
		}
		// A chain's failure passes from each step to the next, its
		// dependent. Through a redundancy group that still has enough
		// healthy members it does not pass, so the chain is contradicted.
		if i+1 < len(s.Chain) && m != nil && store != nil {
			if held, _ := SatisfiedGroup(m, reg, store, s.Chain[i+1].Component, step.Component); held {
				return false
			}
		}
	}
	return true
}

func stepConsistent(step scenarios.Step, store *facts.Store, m *model.Model, reg *providersupport.Registry) bool {
	if store == nil || store.FactsFor(step.Component) == nil {
		return true
	}
	if m == nil || reg == nil {
		return true
	}
	comp := m.Components[step.Component]
	if comp == nil {
		return true
	}
	t, _, err := comp.ResolveType(m, reg)
	if err != nil || t == nil {
		return true
	}
	if store.IsAbsent(step.Component) {
		return absentStepConsistent(step, t)
	}
	return matchedStepConsistent(step, store, m.VarLookup(reg), t)
}

// absentStepConsistent resolves liveness when the probe layer reports
// the component doesn't exist. A component the model says is there and
// the system says is not is a finding, not an all-clear: its default
// ("healthy") state is contradicted, and every failure state stays live --
// absence fits any of them and confirms none, so the failure can still be
// traced through it to the symptom.
func absentStepConsistent(step scenarios.Step, t *providersupport.Type) bool {
	return t.DefaultActiveState == "" || step.State != t.DefaultActiveState
}

// matchedStepConsistent evaluates the step.State's when-predicate
// against the fact store. UnresolvedErrors leave the step live (missing
// facts are "undefined"); evaluator bugs are logged and the step is
// treated as contradicted so a corrupt AST doesn't silently keep every
// scenario alive.
func matchedStepConsistent(step scenarios.Step, store *facts.Store, vars expr.VarLookup, t *providersupport.Type) bool {
	for _, st := range t.States {
		if st.Name != step.State {
			continue
		}
		if st.When == nil {
			return true
		}
		result, err := EvalStatePredicate(st.When, store, vars, step.Component)
		if err != nil {
			var ue *expr.UnresolvedError
			if errors.As(err, &ue) {
				return true
			}
			log.Printf("[liveset] step %s.%s when-predicate error: %v", step.Component, step.State, err)
			return false
		}
		return result
	}
	return true
}

// EvalStatePredicate evaluates a compiled state when-predicate against
// the fact store for a specific component. It mirrors the evaluation
// path used by state.Derive: build an expr.Ctx with the component as
// CurrentComponent and the fact store as the FactLookup, then call the
// node's Eval. An UnresolvedError (or any eval error) is returned to
// the caller unchanged — callers treat "error" as undefined and keep
// the scenario live.
func EvalStatePredicate(node expr.Node, store *facts.Store, vars expr.VarLookup, component string) (bool, error) {
	ctx := expr.Ctx{
		CurrentComponent: component,
		Facts:            store,
		Vars:             vars,
		// States is intentionally nil — live-set filtering runs against
		// the raw fact store, not derived cross-component states. A
		// `state` reference inside a when-predicate will raise an
		// UnresolvedError, which the caller treats as "keep live".
		States: nil,
	}
	result, err := node.Eval(ctx)
	if err != nil {
		var ue *expr.UnresolvedError
		if errors.As(err, &ue) {
			return false, err
		}
		return false, err
	}
	return result, nil
}
