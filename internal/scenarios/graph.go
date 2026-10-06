// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import (
	"fmt"
	"sort"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// Graph is everything enumeration reads from a model and its types: per
// component, the facts it observes, its failure states with what they emit
// and what triggers them, and the components its failure can reach. The
// scenarios are every chain Expand walks through it -- thousands of chains
// from a graph of a few dozen states -- so the graph is what gets stored.
type Graph struct {
	Components map[string]*GraphComponent `yaml:"components"`
}

// GraphComponent is one model component as enumeration sees it.
type GraphComponent struct {
	// Unresolved marks a component whose type did not resolve: no chain
	// starts or passes through it, but it still counts as a dependent.
	Unresolved bool `yaml:"unresolved,omitempty"`
	// Observes lists the type's facts, sorted: what a chain ending here
	// observes. Empty means a chain cannot end here.
	Observes []string `yaml:"observes,omitempty"`
	// States are the type's non-default states, in type order.
	States []GraphState `yaml:"states,omitempty"`
	// Dependents are the components that depend on this one, sorted.
	Dependents []GraphLink `yaml:"dependents,omitempty"`
}

// GraphState is one failure state: the labels it emits to dependents
// (its can_cause) and the labels that can trigger it (empty: any).
type GraphState struct {
	Name        string   `yaml:"name"`
	Emits       []string `yaml:"emits,omitempty"`
	TriggeredBy []string `yaml:"triggered_by,omitempty"`
}

// GraphLink is an edge to a dependent. A plain dependency carries every
// root's failure. One through a redundancy group (need: k) carries only
// the roots in Roots -- those whose failure reaches more members than the
// group can spare.
type GraphLink struct {
	To    string   `yaml:"to"`
	Group bool     `yaml:"group,omitempty"`
	Roots []string `yaml:"roots,omitempty"`
}

// BuildGraph projects m and its resolved types into the enumeration graph.
func BuildGraph(m *model.Model, reg *providersupport.Registry) *Graph {
	inverse := inverseDeps(m)
	reach := dependencyClosure(m)
	roots := sortedComponentNames(m)
	g := &Graph{Components: make(map[string]*GraphComponent, len(m.Components))}
	for _, name := range roots {
		gc := &GraphComponent{}
		g.Components[name] = gc
		t, _, err := m.Components[name].ResolveType(m, reg)
		if err != nil || t == nil {
			gc.Unresolved = true
		} else {
			gc.Observes = factNames(t.Facts)
			for _, st := range t.States {
				if st.Name == t.DefaultActiveState {
					continue
				}
				gc.States = append(gc.States, GraphState{Name: st.Name, Emits: t.FailureModes[st.Name], TriggeredBy: st.TriggeredBy})
			}
		}
		for _, parent := range inverse[name] {
			gc.Dependents = append(gc.Dependents, buildLink(m, reach, roots, parent, name))
		}
	}
	return g
}

// buildLink describes the edge child → parent: plain when the dependency
// propagates for every root, else the roots it carries.
func buildLink(m *model.Model, reach map[string]map[string]bool, roots []string, parent, child string) GraphLink {
	link := GraphLink{To: parent}
	all := true
	var carried []string
	for _, root := range roots {
		if propagates(m, reach, root, parent, child) {
			carried = append(carried, root)
		} else {
			all = false
		}
	}
	if !all {
		link.Group, link.Roots = true, carried
	}
	return link
}

// propagates reports whether root's failure, having reached child, breaks
// parent. A plain dependency always does. A redundancy group (need: k of
// n) does only when root reaches more than n-k of its members -- one
// member down leaves the group holding, a store under every member does
// not.
func propagates(m *model.Model, reach map[string]map[string]bool, root, parent, child string) bool {
	comp := m.Components[parent]
	if comp == nil {
		return true
	}
	for _, dep := range comp.Depends {
		if !containsName(dep.On, child) {
			continue
		}
		if dep.Need <= 0 {
			return true
		}
		hit := 0
		for _, member := range dep.On {
			if member == root || reach[member][root] {
				hit++
			}
		}
		if hit > len(dep.On)-dep.Need {
			return true
		}
	}
	return false
}

// Expand returns every chain in g, sorted, with IDs s-0001... by sort
// position: the scenario set.
//
// Permissive default: when a downstream state has no triggered_by, it
// accepts any upstream can_cause label.
func Expand(g *Graph) []Scenario {
	names := make([]string, 0, len(g.Components))
	for name := range g.Components {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []Scenario
	for _, name := range names {
		gc := g.Components[name]
		if gc.Unresolved {
			continue
		}
		w := walk{g: g, root: name}
		for _, state := range gc.States {
			chains := w.extendChain(name, state, map[string]bool{})
			// A failure no dependent answers to still happened: seen at the
			// root, it is a chain of one, or no strategy could conclude it.
			if len(chains) == 0 && len(gc.Observes) > 0 {
				chains = [][]Step{{{Component: name, State: state.Name, Observes: gc.Observes}}}
			}
			for _, chain := range chains {
				out = append(out, Scenario{Root: RootRef{Component: name, State: state.Name}, Chain: chain})
			}
		}
	}
	sortScenarios(out)
	for i := range out {
		out[i].ID = fmt.Sprintf("s-%04d", i+1)
	}
	return out
}

// walk extends chains for one root through the graph.
type walk struct {
	g    *Graph
	root string
}

func (w walk) extendChain(name string, state GraphState, visited map[string]bool) [][]Step {
	if visited[name] {
		return nil
	}
	visited[name] = true
	defer delete(visited, name)

	gc := w.g.Components[name]
	if gc == nil || gc.Unresolved {
		return nil
	}
	downstreams := w.carriedTo(gc)
	if len(downstreams) == 0 {
		if len(gc.Observes) == 0 {
			return nil
		}
		return [][]Step{{{Component: name, State: state.Name, Observes: gc.Observes}}}
	}
	var out [][]Step
	for _, d := range downstreams {
		gd := w.g.Components[d]
		if gd == nil || gd.Unresolved {
			continue
		}
		out = append(out, w.chainsThrough(name, state, d, gd, visited)...)
	}
	return out
}

// carriedTo lists gc's dependents the root's failure reaches.
func (w walk) carriedTo(gc *GraphComponent) []string {
	var out []string
	for _, link := range gc.Dependents {
		if !link.Group || containsName(link.Roots, w.root) {
			out = append(out, link.To)
		}
	}
	return out
}

// chainsThrough emits every chain that extends (name, state) through the
// dependent d, for each failure state of d that state's labels can
// trigger: the longer chains, and the 2-step chain that stops at d -- a
// real incident may stop at d's symptom layer even when deeper chains
// exist.
func (w walk) chainsThrough(name string, state GraphState, d string, gd *GraphComponent, visited map[string]bool) [][]Step {
	var out [][]Step
	head := Step{Component: name, State: state.Name}
	for _, dstate := range gd.States {
		match := matchEdgeLabel(state.Emits, dstate.TriggeredBy)
		if match == "" {
			continue
		}
		head.EmitsOnEdge = match
		for _, suffix := range w.extendChain(d, dstate, visited) {
			out = append(out, append([]Step{head}, suffix...))
		}
		if len(gd.Observes) > 0 {
			out = append(out, []Step{head, {Component: d, State: dstate.Name, Observes: gd.Observes}})
		}
	}
	return out
}
