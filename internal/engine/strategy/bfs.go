// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"sort"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/state"
)

type bfsStrategy struct{}

// BFS returns the graph-traversal strategy (the pre-scenarios engine
// behavior). Fallback when no scenarios are available.
func BFS() Strategy { return bfsStrategy{} }

func (bfsStrategy) Name() string { return "bfs" }

func (s bfsStrategy) SuggestProbe(in Input) Decision {
	if in.Model == nil {
		return Decision{Stuck: true, Reason: "no model"}
	}
	entry := in.Model.EntryPoint()
	if entry == "" {
		return Decision{Stuck: true, Reason: "model has no entry point"}
	}
	candidates := bfsWalk(in, entry)
	if probe := firstUncollectedProbe(in, candidates); probe != nil {
		return Decision{Probe: probe}
	}
	// BFS has probed every reachable fact. Before declaring "all
	// healthy", check whether any reachable component is standalone
	// unhealthy — a broken upstream with no observed downstream symptom
	// would otherwise be silently reported as healthy.
	if root := standaloneUnhealthyRoot(in, candidates); root != nil {
		return Decision{Done: true, RootCause: root, Reason: "standalone unhealthy component"}
	}
	return Decision{Done: true, Reason: "bfs coverage exhausted"}
}

// bfsWalk returns components reachable from entry in BFS order, honouring
// dependency while-guards exactly as engine.Plan's path enumeration does:
// an edge whose while-guard evaluates definitively false is skipped, so a
// component reachable only through an inactive edge is excluded. Keeping
// this in sync with the engine is what makes BFS-path diagnose and
// simulate agree on which components are in play.
func bfsWalk(in Input, entry string) []string {
	states := deriveComponentStates(in)
	visited := map[string]bool{}
	queue := []string{entry}
	var out []string
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if visited[c] {
			continue
		}
		visited[c] = true
		out = append(out, c)
		comp := in.Model.Components[c]
		if comp == nil {
			continue
		}
		for _, dep := range comp.Depends {
			if !whileGuardActive(dep, c, in.Store, states) {
				continue
			}
			for _, target := range dep.On {
				if !visited[target] {
					queue = append(queue, target)
				}
			}
		}
	}
	return out
}

// deriveComponentStates resolves each component's derived state so
// while-guard expressions referencing `<component>.state` can be
// evaluated. Returns nil when inputs are incomplete (guards then fall
// back to the conservative "walk the edge" branch).
func deriveComponentStates(in Input) map[string]string {
	if in.Model == nil || in.Registry == nil || in.Store == nil {
		return nil
	}
	return state.Derive(in.Model, in.Registry, in.Store).ComponentStates
}

// whileGuardActive reports whether dep's while-guard permits walking the
// edge. Mirrors engine.whileGuardActive: only a definitively-false guard
// skips the edge; an unresolved or errored guard is walked conservatively
// so a missing fact never silently drops a component from the search.
func whileGuardActive(dep model.Dependency, componentName string, store *facts.Store, states map[string]string) bool {
	if dep.While == nil {
		return true
	}
	ctx := expr.Ctx{
		CurrentComponent: componentName,
		Facts:            store,
		States:           states,
	}
	result, err := dep.While.Eval(ctx)
	if !result && err == nil {
		return false // definitively false → skip
	}
	return true
}

// firstUncollectedProbe scans candidates for the first (component,
// fact) pair that has no recorded value yet. Facts for each type are
// iterated in sorted order so output is deterministic.
func firstUncollectedProbe(in Input, candidates []string) *Probe {
	if in.Registry == nil {
		return nil
	}
	for _, compName := range candidates {
		comp := in.Model.Components[compName]
		if comp == nil {
			continue
		}
		t, providerName, err := comp.ResolveType(in.Model, in.Registry)
		if err != nil || t == nil {
			continue
		}
		if p := firstUncollectedFact(in, compName, t, providerName, comp); p != nil {
			return p
		}
	}
	return nil
}

// firstUncollectedFact returns the lowest-sorted fact on t for comp
// that has no Store value yet, packaged as a Probe. Returns nil when
// every fact is already collected.
func firstUncollectedFact(in Input, compName string, t *providersupport.Type, providerName string, comp *model.Component) *Probe {
	names := make([]string, 0, len(t.Facts))
	for n := range t.Facts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, fn := range names {
		if in.Store != nil && in.Store.Latest(compName, fn) != nil {
			continue
		}
		fs := t.Facts[fn]
		if fs == nil {
			continue
		}
		return &Probe{
			Component: compName,
			Fact:      fn,
			Provider:  providerName,
			Type:      t.Name,
			Resource:  comp.Resource,
			Cost:      fs.Probe.Cost,
			Access:    fs.Probe.Access,
			Command:   fs.Probe.Cmd,
			ParseMode: fs.Probe.Parse,
			Vars:      componentVars(in, compName),
		}
	}
	return nil
}
