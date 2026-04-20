package strategy

import (
	"sort"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// standaloneUnhealthyRoot scans all model components and, if any have
// a healthy predicate that evaluates definitively false under the
// collected facts, returns a synthetic one-step Scenario naming the
// most-upstream offender as the root cause.
//
// It exists because BFS (and Occam's "Stuck" exit) terminate without
// reasoning about whether any probed component violates its own
// healthy rule. Without this check, a cluster with an unhealthy
// upstream (e.g. an ExternalSecrets operator whose Deployment is not
// Available) but no observed downstream symptom gets reported as
// "Root cause: (none — all components healthy)" — which is wrong and
// has bitten real incident response.
//
// Root selection: among all definitively-unhealthy components, pick
// those whose direct upstream deps are all healthy (or unresolved)
// — they are the "deepest upstream" unhealth, not downstream
// casualties. Ties broken alphabetically for deterministic output.
// Returns nil when nothing is definitively unhealthy.
func standaloneUnhealthyRoot(in Input) *scenarios.Scenario {
	if in.Model == nil || in.Registry == nil || in.Store == nil {
		return nil
	}
	unhealthy := map[string]bool{}
	for _, name := range in.Model.Order {
		comp := in.Model.Components[name]
		if comp == nil {
			continue
		}
		if componentIsUnhealthy(in, name, comp) {
			unhealthy[name] = true
		}
	}
	if len(unhealthy) == 0 {
		return nil
	}

	var roots []string
	for name := range unhealthy {
		if !hasUnhealthyUpstream(in.Model, name, unhealthy) {
			roots = append(roots, name)
		}
	}
	if len(roots) == 0 {
		// Every unhealthy component has an unhealthy upstream (dep
		// cycle). Fall back to the full unhealthy set so we surface
		// something rather than nothing.
		for name := range unhealthy {
			roots = append(roots, name)
		}
	}
	sort.Strings(roots)
	chosen := roots[0]

	state := failedStateFor(in, chosen)
	return &scenarios.Scenario{
		ID:    "standalone-unhealthy:" + chosen,
		Root:  scenarios.RootRef{Component: chosen, State: state},
		Chain: []scenarios.Step{{Component: chosen, State: state}},
	}
}

func componentIsUnhealthy(in Input, name string, comp *model.Component) bool {
	rules := effectiveHealthy(in, comp)
	if len(rules) == 0 {
		return false
	}
	for _, rule := range rules {
		ok, err := EvalStatePredicate(rule, in.Store, name)
		if err != nil {
			// Unresolved / missing facts → undecidable for this rule.
			// Treat as "don't know" and move on; a component is only
			// flagged when at least one rule is DEFINITIVELY false.
			continue
		}
		if !ok {
			return true
		}
	}
	return false
}

// effectiveHealthy returns the healthy predicate set for a component,
// honouring the component-level override first and falling back to
// the type-level rules from the provider.
func effectiveHealthy(in Input, comp *model.Component) []expr.Node {
	if len(comp.Healthy) > 0 {
		return comp.Healthy
	}
	providers := comp.Providers
	if len(providers) == 0 {
		providers = in.Model.Meta.Providers
	}
	t, _, err := in.Registry.ResolveType(providers, comp.Type)
	if err != nil || t == nil {
		return nil
	}
	return t.Healthy
}

func hasUnhealthyUpstream(m *model.Model, name string, unhealthy map[string]bool) bool {
	comp := m.Components[name]
	if comp == nil {
		return false
	}
	for _, dep := range comp.Depends {
		for _, target := range dep.On {
			if unhealthy[target] {
				return true
			}
		}
	}
	return false
}

// failedStateFor returns the name of the first non-default state
// whose When predicate is satisfied — or "unhealthy" if no state
// matches (or the type has no states declared). Used as a label on
// the synthetic scenario so the report reads like a real chain.
func failedStateFor(in Input, name string) string {
	comp := in.Model.Components[name]
	if comp == nil {
		return "unhealthy"
	}
	providers := comp.Providers
	if len(providers) == 0 {
		providers = in.Model.Meta.Providers
	}
	t, _, err := in.Registry.ResolveType(providers, comp.Type)
	if err != nil || t == nil {
		return "unhealthy"
	}
	for _, st := range t.States {
		if st.Name == t.DefaultActiveState || st.When == nil {
			continue
		}
		ok, err := EvalStatePredicate(st.When, in.Store, name)
		if err == nil && ok {
			return st.Name
		}
	}
	return "unhealthy"
}
