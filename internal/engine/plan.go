// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"errors"
	"fmt"
	"log"
	"sort"

	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
	"github.com/mgt-tool/mgtt/internal/state"
)

// Plan runs the 5-stage constraint engine against the model and fact store,
// returning a PathTree describing all failure paths, eliminated paths, and
// (if determinable) the root cause.
//
// If entry is non-empty it is used as the starting component; otherwise
// the model's EntryPoint (first component with in-degree 0) is used.
func Plan(m *model.Model, reg *providersupport.Registry, store *facts.Store, entry string) *PathTree {
	return PlanWith(m, reg, store, entry, nil)
}

// PlanWith is Plan with operator hints forwarded to the strategy. Suspects
// bias probe ordering toward the agent's suspicion; an empty slice is the
// no-hint case.
func PlanWith(m *model.Model, reg *providersupport.Registry, store *facts.Store, entry string, suspects []strategy.SuspectHint) *PathTree {
	if entry == "" {
		entry = m.EntryPoint()
	}
	// Stage 1 — state derivation must precede path enumeration so while-
	// guard expressions on dependency edges can reference derived states.
	derivation := state.Derive(m, reg, store)
	// Stage 2 — walk the dep graph.
	paths := enumeratePaths(m, entry, store, derivation)
	// Stage 3 — split alive vs eliminated, annotate reason strings.
	alive, eliminated := splitPaths(paths, m, reg, store, derivation)

	tree := &PathTree{
		Entry:      entry,
		Paths:      alive,
		Eliminated: eliminated,
		States:     derivation,
		// Stage 4 — deepest surviving path's tail is the root cause.
		RootCause: pickRootCause(alive),
	}
	// Stage 5 — strategy dispatch.
	tree.Suggested = suggestNextProbe(m, reg, store, suspects)
	return tree
}

// splitPaths annotates each path's ID, computes eliminated/alive sets,
// and writes the Reason strings for eliminated entries.
func splitPaths(paths []Path, m *model.Model, reg *providersupport.Registry, store *facts.Store, derivation *state.Derivation) (alive, eliminated []Path) {
	for i, p := range paths {
		p.ID = fmt.Sprintf("PATH %c", 'A'+i)
		deepest := p.Components[len(p.Components)-1]
		deepestState := derivation.ComponentStates[deepest]

		if !isEliminated(m, reg, store, deepest) {
			alive = append(alive, p)
			continue
		}
		// The deepest component is eliminable. When it has NO facts and an
		// upstream component is known-unhealthy, keep the path alive so
		// the engine continues probing inward; otherwise it's unchecked.
		// When it has facts and isn't definitively unhealthy, it's proven
		// healthy.
		if store.FactsFor(deepest) == nil {
			if hasUnhealthyUpstream(p, m, reg, store, derivation) {
				alive = append(alive, p)
				continue
			}
			p.Reason = fmt.Sprintf("%s has no observations", deepest)
		} else {
			p.Reason = fmt.Sprintf("%s healthy (%s)", deepest, deepestState)
		}
		eliminated = append(eliminated, p)
	}
	return alive, eliminated
}

// hasUnhealthyUpstream reports whether any ancestor of the deepest
// component on p has facts AND is in a non-default state.
func hasUnhealthyUpstream(p Path, m *model.Model, reg *providersupport.Registry, store *facts.Store, derivation *state.Derivation) bool {
	for _, c := range p.Components[:len(p.Components)-1] {
		cDefault := ResolveDefaultActive(m.Components[c], m, reg)
		cState := derivation.ComponentStates[c]
		if store.FactsFor(c) != nil && (cDefault == "" || cState != cDefault) {
			return true
		}
	}
	return false
}

// pickRootCause returns the deepest-component name on the longest alive
// path. Empty string when there are no alive paths.
//
// It must NOT reorder alive: that slice shares its backing array with
// PathTree.Paths, which enumeratePaths has already sorted into
// declaration order for deterministic output. A single max-scan keeping
// the first (declaration-order-earliest) longest path preserves that
// ordering and breaks length ties deterministically.
func pickRootCause(alive []Path) string {
	if len(alive) == 0 {
		return ""
	}
	best := alive[0]
	for _, p := range alive[1:] {
		if len(p.Components) > len(best.Components) {
			best = p
		}
	}
	return best.Components[len(best.Components)-1]
}

// suggestNextProbe runs the strategy dispatcher against the current
// store + scenarios. AutoSelect returns Occam when scenarios are
// available, BFS otherwise.
func suggestNextProbe(m *model.Model, reg *providersupport.Registry, store *facts.Store, suspects []strategy.SuspectHint) *Probe {
	input := strategy.Input{
		Model:     m,
		Registry:  reg,
		Store:     store,
		Scenarios: loadScenariosIfPresent(m),
		Suspects:  suspects,
	}
	return decisionToProbe(strategy.AutoSelect(input).SuggestProbe(input))
}

// decisionToProbe unpacks a strategy.Decision. engine.Probe is now a
// type alias for strategy.Probe, so the returned pointer is the same
// struct the strategy produced — no field-by-field copy, no dropped
// Type/Resource/Vars.
func decisionToProbe(d strategy.Decision) *Probe { return d.Probe }

// loadScenariosIfPresent delegates to scenarios.LoadSiblingOf, swallowing
// any error (parse failure, permission) so the engine continues with an
// empty scenario set and falls back to BFS. SourcePath-less models are a
// no-op by design.
func loadScenariosIfPresent(m *model.Model) []scenarios.Scenario {
	if m == nil || m.SourcePath == "" {
		return nil
	}
	scs, _, _ := scenarios.LoadSiblingOf(m.SourcePath)
	return scs
}

// enumeratePaths does a BFS from entry through the dependency graph and
// returns one Path per reachable component (excluding entry as a trivial
// single-component path). Each path is the shortest walk from entry to
// that component.
//
// Dependency edges with a while guard are evaluated against the current
// derived states and fact store:
//   - while == nil           → always active (walk the edge)
//   - while evals (true,nil) → active (walk the edge)
//   - while evals (false,nil)→ inactive (skip the edge)
//   - while evals (false, *UnresolvedError) → conservative, walk the edge
func enumeratePaths(m *model.Model, entry string, store *facts.Store, derivation *state.Derivation) []Path {
	paths := bfsEnumerate(m, entry, store, derivation)
	sortPathsByDeclarationOrder(paths, m.Order)
	return paths
}

type bfsItem struct {
	name string
	path []string
}

// bfsEnumerate walks the dependency graph from entry, applying
// while-guard filtering on each edge. Returns every reachable
// non-trivial path (each path is the shortest walk from entry to its
// terminal component).
func bfsEnumerate(m *model.Model, entry string, store *facts.Store, derivation *state.Derivation) []Path {
	visited := map[string]bool{entry: true}
	queue := []bfsItem{{name: entry, path: []string{entry}}}
	var paths []Path
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		comp := m.Components[curr.name]
		if comp == nil {
			continue
		}
		for _, dep := range comp.Depends {
			if !whileGuardActive(dep, curr.name, store, derivation) {
				continue
			}
			for _, target := range dep.On {
				if visited[target] {
					continue
				}
				visited[target] = true
				newPath := append(append([]string(nil), curr.path...), target)
				paths = append(paths, Path{Components: newPath})
				queue = append(queue, bfsItem{name: target, path: newPath})
			}
		}
	}
	return paths
}

// whileGuardActive reports whether dep's while-guard (if any) permits
// walking the edge. Conservative: unresolved or unexpected eval errors
// log and still walk rather than silently dropping paths.
func whileGuardActive(dep model.Dependency, componentName string, store *facts.Store, derivation *state.Derivation) bool {
	if dep.While == nil {
		return true
	}
	ctx := expr.Ctx{
		CurrentComponent: componentName,
		Facts:            store,
		States:           derivation.ComponentStates,
	}
	result, evalErr := dep.While.Eval(ctx)
	if !result && evalErr == nil {
		return false // condition definitively false — skip
	}
	var ue *expr.UnresolvedError
	if evalErr != nil && !errors.As(evalErr, &ue) {
		log.Printf("engine: while-guard eval error on %s → %v: %v", componentName, dep.On, evalErr)
	}
	return true
}

// sortPathsByDeclarationOrder sorts paths by their terminal component's
// position in m.Order for deterministic output.
func sortPathsByDeclarationOrder(paths []Path, order []string) {
	orderIdx := make(map[string]int, len(order))
	for i, name := range order {
		orderIdx[name] = i
	}
	sort.Slice(paths, func(i, j int) bool {
		ti := paths[i].Components[len(paths[i].Components)-1]
		tj := paths[j].Components[len(paths[j].Components)-1]
		return orderIdx[ti] < orderIdx[tj]
	})
}

// EliminatedOnly returns the components that appear on eliminated paths but
// never on a surviving path. Order is deterministic (declaration order on
// first occurrence, deduped).
func EliminatedOnly(tree *PathTree) []string {
	surviving := map[string]bool{}
	for _, p := range tree.Paths {
		for _, c := range p.Components {
			surviving[c] = true
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range tree.Eliminated {
		for _, c := range p.Components {
			if !surviving[c] && !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// ResolveDefaultActive looks up the default_active_state for a component's
// type via the registry, honouring the component's effective providers list.
func ResolveDefaultActive(comp *model.Component, m *model.Model, reg *providersupport.Registry) string {
	t, _, err := comp.ResolveType(m, reg)
	if err != nil {
		return ""
	}
	return t.DefaultActiveState
}

// isEliminated determines whether a path's deepest component should be
// eliminated (proven not the root cause). A component is eliminated if:
//   - It has NO facts at all (unchecked — can't be blamed for observed
//     symptoms), OR
//   - It is not DEFINITIVELY unhealthy per its canonical health verdict.
//
// Health is decided by strategy.ComponentDefinitivelyUnhealthy — the same
// verdict the live diagnose strategies use — so simulate and diagnose can
// never disagree on whether a component is broken.
func isEliminated(m *model.Model, reg *providersupport.Registry, store *facts.Store, component string) bool {
	// No facts at all → unchecked → eliminate.
	if store.FactsFor(component) == nil {
		return true
	}
	// Has facts: keep alive only when definitively unhealthy.
	return !strategy.ComponentDefinitivelyUnhealthy(m, reg, store, component)
}
