package strategy

import (
	"errors"

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

func isLive(s scenarios.Scenario, store *facts.Store, m *model.Model, reg *providersupport.Registry) bool {
	for _, step := range s.Chain {
		if !stepConsistent(step, store, m, reg) {
			return false
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
	providers := comp.Providers
	if len(providers) == 0 {
		providers = m.Meta.Providers
	}
	t, _, err := reg.ResolveType(providers, comp.Type)
	if err != nil || t == nil {
		return true
	}
	for _, st := range t.States {
		if st.Name != step.State {
			continue
		}
		if st.When == nil {
			return true
		}
		result, err := evalStatePredicate(st.When, store, step.Component)
		if err != nil {
			return true // undefined / missing-fact → keep live
		}
		return result // true = confirmed, false = contradicted
	}
	return true
}

// evalStatePredicate evaluates a compiled state when-predicate against
// the fact store for a specific component. It mirrors the evaluation
// path used by state.Derive: build an expr.Ctx with the component as
// CurrentComponent and the fact store as the FactLookup, then call the
// node's Eval. An UnresolvedError (or any eval error) is returned to
// the caller unchanged — callers treat "error" as undefined and keep
// the scenario live.
func evalStatePredicate(node expr.Node, store *facts.Store, component string) (bool, error) {
	ctx := expr.Ctx{
		CurrentComponent: component,
		Facts:            store,
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
