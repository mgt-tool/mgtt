// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"sort"

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
func standaloneUnhealthyRoot(in Input, reachable []string) *scenarios.Scenario {
	if in.Model == nil || in.Registry == nil || in.Store == nil {
		return nil
	}
	// Only consider components reachable under the active topology
	// (while-guards applied). engine.Plan can't blame a component that
	// isn't on any active path; restricting the scan keeps the BFS-path
	// verdict consistent with it.
	reach := make(map[string]bool, len(reachable))
	for _, n := range reachable {
		reach[n] = true
	}
	unhealthy := map[string]bool{}
	for _, name := range in.Model.Order {
		if !reach[name] {
			continue
		}
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

func componentIsUnhealthy(in Input, name string, _ *model.Component) bool {
	// Delegates to the canonical verdict (see health.go) so the live
	// strategies and the path engine (engine.Plan) agree on what
	// "unhealthy" means.
	return ComponentDefinitivelyUnhealthy(in.Model, in.Registry, in.Store, name)
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
	t, _, err := comp.ResolveType(in.Model, in.Registry)
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
