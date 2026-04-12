package state

import (
	"errors"

	"github.com/sajonaro/mgtt/internal/expr"
	factspkg "github.com/sajonaro/mgtt/internal/facts"
	"github.com/sajonaro/mgtt/internal/model"
	"github.com/sajonaro/mgtt/internal/providersupport"
)

// Derivation holds the results of a state derivation pass over all components.
type Derivation struct {
	// ComponentStates maps component name → derived state name.
	// If no state matched, the value is "unknown".
	ComponentStates map[string]string

	// UnresolvedBy maps component name → list of UnresolvedErrors encountered
	// while evaluating that component's state expressions. Non-empty means at
	// least one state condition could not be evaluated due to missing facts.
	UnresolvedBy map[string][]expr.UnresolvedError
}

// Derive evaluates the state of every component in m against the fact store.
//
// For each component the ordered state list is walked; the first state whose
// When expression evaluates to (true, nil) wins. If a When expression returns
// (false, *UnresolvedError) the error is recorded and evaluation continues to
// the next state (fall-through). If no state matches, the component's state is
// set to "unknown".
func Derive(m *model.Model, reg *providersupport.Registry, store *factspkg.Store) *Derivation {
	d := &Derivation{
		ComponentStates: make(map[string]string, len(m.Components)),
		UnresolvedBy:    make(map[string][]expr.UnresolvedError),
	}

	// We process components in declaration order so that cross-component state
	// references (e.g. api.state == "live") resolve correctly for earlier
	// components. This is best-effort — cycles aren't handled here.
	for _, name := range m.Order {
		comp := m.Components[name]
		d.ComponentStates[name] = deriveOne(name, comp, m.Meta.Providers, reg, store, d.ComponentStates, d.UnresolvedBy)
	}

	return d
}

// deriveOne derives the state for a single component and returns the state name.
func deriveOne(
	name string,
	comp *model.Component,
	metaProviders []string,
	reg *providersupport.Registry,
	store *factspkg.Store,
	partialStates map[string]string,
	unresolvedBy map[string][]expr.UnresolvedError,
) string {
	// Resolve component type → get ordered states.
	// Use component-level providers if set, otherwise fall back to model-level.
	providers := comp.Providers
	if len(providers) == 0 {
		providers = metaProviders
	}

	t, _, err := reg.ResolveType(providers, comp.Type)
	if err != nil {
		// Can't resolve type — unknown.
		return "unknown"
	}

	ctx := expr.Ctx{
		CurrentComponent: name,
		Facts:            store,
		States:           partialStates,
	}

	for _, sd := range t.States {
		if sd.When == nil {
			// No condition — skip.
			continue
		}

		result, evalErr := sd.When.Eval(ctx)

		if result && evalErr == nil {
			// First match wins.
			return sd.Name
		}

		var ue *expr.UnresolvedError
		if errors.As(evalErr, &ue) {
			// Record the unresolved error and continue to the next state.
			unresolvedBy[name] = append(unresolvedBy[name], *ue)
			continue
		}

		// evalErr is non-nil but not an UnresolvedError (e.g. type mismatch) —
		// skip this state silently.
		if evalErr != nil {
			continue
		}

		// result == false, evalErr == nil → condition definitively false, continue.
	}

	return "unknown"
}
