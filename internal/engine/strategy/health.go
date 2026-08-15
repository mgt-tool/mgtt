// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// ComponentDefinitivelyUnhealthy is the single canonical health verdict
// shared by the path engine (engine.Plan) and the live strategies. A
// component is unhealthy iff at least one of its effective healthy rules
// (component-level override, else the provider type's rules) evaluates
// DEFINITIVELY false under the collected facts. Unresolved/undecidable
// rules are treated as "don't know" — a component is never flagged on a
// rule the facts can't decide.
//
// Keying both the simulate path and the live-diagnose path off this one
// function is what makes "one engine, two contexts" true: the model that
// passes in CI is reasoned about identically when it runs at 3am.
func ComponentDefinitivelyUnhealthy(m *model.Model, reg *providersupport.Registry, store *facts.Store, name string) bool {
	if m == nil || reg == nil || store == nil {
		return false
	}
	comp := m.Components[name]
	if comp == nil {
		return false
	}
	rules := effectiveHealthyFor(m, reg, comp)
	if len(rules) == 0 {
		return false
	}
	for _, rule := range rules {
		ok, err := EvalStatePredicate(rule, store, name)
		if err != nil {
			// Unresolved / missing facts → undecidable for this rule.
			continue
		}
		if !ok {
			return true
		}
	}
	return false
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
