package engine

import (
	"fmt"
	"os"
	"path/filepath"
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
	if entry == "" {
		entry = m.EntryPoint()
	}

	// State derivation must precede path enumeration so while-guard
	// expressions on dependency edges can reference derived states.
	derivation := state.Derive(m, reg, store)
	paths := enumeratePaths(m, entry, store, derivation)

	var alive []Path
	var eliminated []Path

	for i, p := range paths {
		p.ID = fmt.Sprintf("PATH %c", 'A'+i)

		// The deepest (last) component on the path determines path fate.
		deepest := p.Components[len(p.Components)-1]
		deepestState := derivation.ComponentStates[deepest]

		comp := m.Components[deepest]
		defaultActive := resolveDefaultActive(comp, m.Meta.Providers, reg)

		if isEliminated(deepest, deepestState, defaultActive, store) {
			// The deepest component is eliminable. If it has no facts (unchecked)
			// and an intermediate component is known-unhealthy, keep the path alive
			// so the engine continues probing inward. However, if the deepest
			// component is proven healthy (has facts, state == default_active),
			// always eliminate — the path is cleared.
			keptAlive := false
			if store.FactsFor(deepest) == nil {
				for _, c := range p.Components[:len(p.Components)-1] {
					cState := derivation.ComponentStates[c]
					cComp := m.Components[c]
					cDefault := resolveDefaultActive(cComp, m.Meta.Providers, reg)
					if store.FactsFor(c) != nil && (cDefault == "" || cState != cDefault) {
						keptAlive = true
						break
					}
				}
			}
			if keptAlive {
				alive = append(alive, p)
			} else {
				if deepestState == defaultActive {
					p.Reason = fmt.Sprintf("%s healthy (%s)", deepest, deepestState)
				} else {
					p.Reason = fmt.Sprintf("%s has no observations", deepest)
				}
				eliminated = append(eliminated, p)
			}
		} else {
			alive = append(alive, p)
		}
	}

	tree := &PathTree{
		Entry:      entry,
		Paths:      alive,
		Eliminated: eliminated,
		States:     derivation,
	}

	// Stage 4 — Root cause identification
	// Among surviving paths, the one with the deepest unhealthy component
	// (longest path) is the root cause path.
	if len(alive) > 0 {
		// Sort alive paths by length descending — longest path has the
		// deepest root cause.
		sort.Slice(alive, func(i, j int) bool {
			return len(alive[i].Components) > len(alive[j].Components)
		})
		bestPath := alive[0]
		tree.RootCause = bestPath.Components[len(bestPath.Components)-1]
	}

	// Stage 5 — Probe suggestion.
	// Pick the next fact to collect. When scenarios are available for
	// this model (loaded from scenarios.yaml beside it, if any), delegate
	// to strategy.Occam which prefers shortest scenarios and tie-breaks
	// by --suspect / cross-elimination count. When no scenarios are
	// available, keep the legacy path-tree-aware BFS behavior so existing
	// engine tests pass bit-for-bit.
	scs := loadScenariosIfPresent(m)
	if len(scs) > 0 {
		input := strategy.Input{
			Model:     m,
			Registry:  reg,
			Store:     store,
			Scenarios: scs,
		}
		strat := strategy.AutoSelect(input)
		dec := strat.SuggestProbe(input)
		tree.Suggested = decisionToProbe(dec)
	} else {
		tree.Suggested = suggestProbe(m, reg, store, tree)
	}

	return tree
}

// decisionToProbe adapts a strategy.Decision into the *Probe type the
// PathTree exposes to CLI renderers. Done/Stuck decisions with no
// probe return nil — the caller already handles nil Suggested.
func decisionToProbe(d strategy.Decision) *Probe {
	if d.Probe == nil {
		return nil
	}
	return &Probe{
		Component:  d.Probe.Component,
		Fact:       d.Probe.Fact,
		Provider:   d.Probe.Provider,
		ParseMode:  d.Probe.ParseMode,
		Eliminates: d.Probe.Eliminates,
		Cost:       d.Probe.Cost,
		Access:     d.Probe.Access,
		Command:    d.Probe.Command,
	}
}

// loadScenariosIfPresent reads scenarios.yaml from the model's source
// directory when available, returning an empty slice on any miss. The
// model file path is tracked on Model.SourcePath if set by the loader;
// otherwise this is a no-op and the BFS fallback path is used.
func loadScenariosIfPresent(m *model.Model) []scenarios.Scenario {
	if m == nil || m.SourcePath == "" {
		return nil
	}
	p := filepath.Join(filepath.Dir(m.SourcePath), "scenarios.yaml")
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	scs, _, err := scenarios.Read(f)
	if err != nil {
		return nil
	}
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
	type bfsItem struct {
		name string
		path []string
	}

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
			// Evaluate the while guard if present.
			if dep.While != nil {
				ctx := expr.Ctx{
					CurrentComponent: curr.name,
					Facts:            store,
					States:           derivation.ComponentStates,
				}
				result, evalErr := dep.While.Eval(ctx)
				if !result && evalErr == nil {
					// Condition is definitively false → skip this edge.
					continue
				}
				// (true, nil) → walk; (false, *UnresolvedError) → conservative, walk.
				// Any other error → also walk conservatively.
				_ = evalErr
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

	// Sort paths by terminal component's declaration order for determinism.
	orderIdx := make(map[string]int, len(m.Order))
	for i, name := range m.Order {
		orderIdx[name] = i
	}
	sort.Slice(paths, func(i, j int) bool {
		ti := paths[i].Components[len(paths[i].Components)-1]
		tj := paths[j].Components[len(paths[j].Components)-1]
		return orderIdx[ti] < orderIdx[tj]
	})

	return paths
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
// type, falling back to the model-level providers list.
func ResolveDefaultActive(comp *model.Component, metaProviders []string, reg *providersupport.Registry) string {
	providers := comp.Providers
	if len(providers) == 0 {
		providers = metaProviders
	}
	t, _, err := reg.ResolveType(providers, comp.Type)
	if err != nil {
		return ""
	}
	return t.DefaultActiveState
}

// resolveDefaultActive is the package-internal alias kept for call sites here.
func resolveDefaultActive(comp *model.Component, metaProviders []string, reg *providersupport.Registry) string {
	return ResolveDefaultActive(comp, metaProviders, reg)
}

// suggestProbe picks the next fact to collect. It walks all components that
// appear on surviving (non-eliminated) paths — starting from the entry point
// and proceeding inward — and returns the first uncollected fact it finds.
//
// If no surviving paths exist or every reachable component already has all
// facts collected, nil is returned (nothing left to probe).
func suggestProbe(m *model.Model, reg *providersupport.Registry, store *facts.Store, tree *PathTree) *Probe {
	// Collect unique components from surviving paths in BFS order.
	// Include entry even if it's not the terminal of any path (it may have uncollected facts).
	seen := map[string]bool{}
	var candidates []string

	// Entry first.
	if store.FactsFor(tree.Entry) == nil {
		candidates = append(candidates, tree.Entry)
		seen[tree.Entry] = true
	}

	// Then components from surviving paths, shallowest first.
	for _, p := range tree.Paths {
		for _, c := range p.Components {
			if !seen[c] {
				seen[c] = true
				candidates = append(candidates, c)
			}
		}
	}

	// If no surviving paths and no entry to probe, nothing to suggest.
	if len(candidates) == 0 {
		return nil
	}

	for _, compName := range candidates {
		comp := m.Components[compName]
		if comp == nil {
			continue
		}
		providers := comp.Providers
		if len(providers) == 0 {
			providers = m.Meta.Providers
		}
		t, providerName, err := reg.ResolveType(providers, comp.Type)
		if err != nil {
			continue
		}

		// Sort fact names for deterministic ordering.
		var factNames []string
		for fn := range t.Facts {
			factNames = append(factNames, fn)
		}
		sort.Strings(factNames)

		for _, fn := range factNames {
			if store.Latest(compName, fn) != nil {
				continue // already collected
			}
			fs := t.Facts[fn]

			// Determine which paths this probe would help eliminate.
			var eliminates []string
			for _, p := range tree.Paths {
				for _, c := range p.Components {
					if c == compName {
						eliminates = append(eliminates, p.ID)
						break
					}
				}
			}

			return &Probe{
				Component:  compName,
				Fact:       fn,
				Provider:   providerName,
				ParseMode:  fs.Probe.Parse,
				Eliminates: eliminates,
				Cost:       fs.Probe.Cost,
				Access:     fs.Probe.Access,
				Command:    fs.Probe.Cmd,
			}
		}
	}

	return nil
}

// isEliminated determines whether a path's deepest component should be eliminated.
// A component is eliminated if:
//   - Its state matches the default_active_state (proven healthy), OR
//   - It has NO facts at all (unchecked — can't be blamed for observed symptoms)
func isEliminated(component, currentState, defaultActive string, store *facts.Store) bool {
	// If the component is in the default active state → healthy → eliminate.
	if currentState == defaultActive && defaultActive != "" {
		return true
	}

	// If the component has NO facts at all, it's unchecked → eliminate.
	if store.FactsFor(component) == nil {
		return true
	}

	return false
}
