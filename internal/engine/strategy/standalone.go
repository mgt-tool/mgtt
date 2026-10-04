// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
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
// Root selection is RootCause, the rule engine.Plan uses too: a broken
// component no other broken component could have caused, the most
// upstream first. Returns nil when nothing in play is seen broken.
func standaloneUnhealthyRoot(in Input, _ []string) *scenarios.Scenario {
	if in.Model == nil || in.Registry == nil || in.Store == nil {
		return nil
	}
	chosen := RootCause(in, in.Model.EntryPoint())
	if chosen == "" {
		return nil
	}

	state := failedStateFor(in, chosen)
	return &scenarios.Scenario{
		ID:    "standalone-unhealthy:" + chosen,
		Root:  scenarios.RootRef{Component: chosen, State: state},
		Chain: []scenarios.Step{{Component: chosen, State: state}},
	}
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
		ok, err := EvalStatePredicate(st.When, in.Store, in.Model.VarLookup(in.Registry), name)
		if err == nil && ok {
			return st.Name
		}
	}
	return "unhealthy"
}
