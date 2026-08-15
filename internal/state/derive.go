// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package state

import (
	"errors"

	"github.com/mgt-tool/mgtt/internal/expr"
	factspkg "github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
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
		d.ComponentStates[name] = deriveOne(name, comp, m, reg, store, d.ComponentStates, d.UnresolvedBy)
	}

	return d
}

// deriveOne derives the state for a single component and returns the state name.
func deriveOne(
	name string,
	comp *model.Component,
	m *model.Model,
	reg *providersupport.Registry,
	store *factspkg.Store,
	partialStates map[string]string,
	unresolvedBy map[string][]expr.UnresolvedError,
) string {
	t, _, err := comp.ResolveType(m, reg)
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
			continue
		}
		if matched := tryStateMatch(name, sd, ctx, unresolvedBy); matched {
			return sd.Name
		}
	}
	return "unknown"
}

// tryStateMatch evaluates one StateDef's When expression. Returns true
// when the state matches definitively. Records UnresolvedErrors against
// the component name so callers can surface blocked derivations. Other
// eval errors (type mismatch, etc.) are swallowed and treated as "skip
// this state" — the only way a state can win is a true/nil result.
func tryStateMatch(name string, sd providersupport.StateDef, ctx expr.Ctx, unresolvedBy map[string][]expr.UnresolvedError) bool {
	result, evalErr := sd.When.Eval(ctx)
	if result && evalErr == nil {
		return true
	}
	var ue *expr.UnresolvedError
	if errors.As(evalErr, &ue) {
		unresolvedBy[name] = append(unresolvedBy[name], *ue)
	}
	return false
}
