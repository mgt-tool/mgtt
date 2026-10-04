// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// Verdict is what the collected facts prove about a component's health.
type Verdict int

const (
	// Unknown: the facts decide nothing either way. A forbidden, transient
	// or missing fact lands here, never in Healthy.
	Unknown Verdict = iota
	// Healthy: every effective healthy rule resolved, and all are true.
	Healthy
	// Unhealthy: at least one effective healthy rule resolved false.
	Unhealthy
)

// ComponentVerdict is the single canonical health verdict shared by the
// path engine (engine.Plan) and the live strategies. Unhealthy needs one
// rule definitively false; Healthy needs every rule definitively true.
// Anything in between is Unknown, so a fact the probes could not read can
// keep a component in play but can never clear it.
//
// Keying both the simulate path and the live-diagnose path off this one
// function is what makes "one engine, two contexts" true: the model that
// passes in CI is reasoned about identically when it runs at 3am.
func ComponentVerdict(m *model.Model, reg *providersupport.Registry, store *facts.Store, name string) Verdict {
	if m == nil || reg == nil || store == nil {
		return Unknown
	}
	comp := m.Components[name]
	if comp == nil {
		return Unknown
	}
	rules := effectiveHealthyFor(m, reg, comp)
	if len(rules) == 0 {
		// Nothing can fail, so an observed component is as healthy as it
		// will ever be shown; an unobserved one stays Unknown.
		if observed(store, name) {
			return Healthy
		}
		return Unknown
	}
	verdict := Healthy
	for _, rule := range rules {
		ok, err := EvalStatePredicate(rule, store, name)
		switch {
		case err != nil:
			verdict = Unknown
		case !ok:
			return Unhealthy
		}
	}
	return verdict
}

// observed reports whether at least one fact for name resolved to a value.
func observed(store *facts.Store, name string) bool {
	for _, f := range store.FactsFor(name) {
		if f.Value != nil {
			return true
		}
	}
	return false
}

// ComponentDefinitivelyUnhealthy reports whether the facts prove name
// unhealthy. Rules the facts can't decide never flag a component.
func ComponentDefinitivelyUnhealthy(m *model.Model, reg *providersupport.Registry, store *facts.Store, name string) bool {
	return ComponentVerdict(m, reg, store, name) == Unhealthy
}

// effectiveHealthyFor returns the healthy predicate set for a component,
// honouring the component-level override first and falling back to the
// type-level rules from the provider.
func effectiveHealthyFor(m *model.Model, reg *providersupport.Registry, comp *model.Component) []expr.Node {
	if len(comp.Healthy) > 0 {
		return comp.Healthy
	}
	t, _, err := comp.ResolveType(m, reg)
	if err != nil || t == nil {
		return nil
	}
	return t.Healthy
}
