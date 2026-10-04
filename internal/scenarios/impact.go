// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mgt-tool/mgtt/internal/model"
)

// Impact answers "what breaks if this component fails?": every component
// its failure can reach through the failure graph, by the same rules the
// scenarios follow -- can_cause labels must trigger the next state, and a
// redundancy group that the failure does not overwhelm stops it.
type Impact struct {
	Component string
	States    []string // the failure states assumed
	Affected  []Affected
	Blocked   []Blocked
}

// Affected is one component the failure reaches.
type Affected struct {
	Component string
	States    []string // the states it can be driven into
	Path      []string // a shortest chain, from the failed component to this one
	// Symptom marks a component nothing depends on: where the failure
	// shows to users.
	Symptom bool
	// Conditions are set when every way the failure reaches this
	// component crosses a while: guard: it breaks only while one of
	// them holds. Empty means some way needs no guard.
	Conditions []string
}

// Blocked is where a redundancy group stops the failure: Dependent still
// has enough healthy members besides Member.
type Blocked struct {
	Dependent string
	Member    string
}

type impactNode struct{ comp, state string }

// ImpactOf walks g from component's failure states: all of them, or only
// those named. m supplies the while: guards to report; it may be nil.
func ImpactOf(g *Graph, m *model.Model, component string, states []string) (*Impact, error) {
	gc := g.Components[component]
	if gc == nil {
		return nil, fmt.Errorf("no component %q in the model", component)
	}
	if gc.Unresolved {
		return nil, fmt.Errorf("component %q has no resolved type, so no failure states", component)
	}
	byName := map[string]GraphState{}
	var all []string
	for _, st := range gc.States {
		byName[st.Name] = st
		all = append(all, st.Name)
	}
	if len(states) == 0 {
		states = all
	}
	imp := &Impact{Component: component, States: states}
	parent := map[impactNode]impactNode{}
	stateOf := map[impactNode]GraphState{}
	var queue []impactNode
	for _, s := range states {
		st, ok := byName[s]
		if !ok {
			return nil, fmt.Errorf("%q is not a failure state of %s (its failure states: %s)", s, component, strings.Join(all, ", "))
		}
		n := impactNode{component, s}
		stateOf[n], parent[n] = st, n
		queue = append(queue, n)
	}

	reached := map[string]*Affected{}
	blocked := map[Blocked]bool{}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, link := range g.Components[cur.comp].Dependents {
			if link.Group && !containsName(link.Roots, component) {
				blocked[Blocked{Dependent: link.To, Member: cur.comp}] = true
				continue
			}
			gd := g.Components[link.To]
			if gd == nil || gd.Unresolved || link.To == component {
				continue
			}
			for _, dstate := range gd.States {
				if matchEdgeLabel(stateOf[cur].Emits, dstate.TriggeredBy) == "" {
					continue
				}
				next := impactNode{link.To, dstate.Name}
				if _, seen := parent[next]; seen {
					continue
				}
				parent[next], stateOf[next] = cur, dstate
				queue = append(queue, next)
				a := reached[link.To]
				if a == nil {
					a = &Affected{Component: link.To, Path: pathTo(next, parent), Symptom: len(gd.Dependents) == 0}
					reached[link.To] = a
				}
				a.States = append(a.States, dstate.Name)
			}
		}
	}

	free := unguardedReach(g, m, component, parent)
	for _, a := range reached {
		if !free[a.Component] {
			a.Conditions = conditionsFor(m, a.Path, free, reached, component)
		}
	}
	for _, a := range reached {
		sort.Strings(a.States)
		imp.Affected = append(imp.Affected, *a)
	}
	sort.Slice(imp.Affected, func(i, j int) bool {
		if len(imp.Affected[i].Path) != len(imp.Affected[j].Path) {
			return len(imp.Affected[i].Path) < len(imp.Affected[j].Path)
		}
		return imp.Affected[i].Component < imp.Affected[j].Component
	})
	for b := range blocked {
		// A group stops this member's failure, but if the failure reaches
		// the dependent another way it is not blocked after all.
		if reached[b.Dependent] == nil {
			imp.Blocked = append(imp.Blocked, b)
		}
	}
	sort.Slice(imp.Blocked, func(i, j int) bool {
		if imp.Blocked[i].Dependent != imp.Blocked[j].Dependent {
			return imp.Blocked[i].Dependent < imp.Blocked[j].Dependent
		}
		return imp.Blocked[i].Member < imp.Blocked[j].Member
	})
	return imp, nil
}

// pathTo walks parent links back to the failed component.
func pathTo(n impactNode, parent map[impactNode]impactNode) []string {
	var rev []string
	for {
		rev = append(rev, n.comp)
		p := parent[n]
		if p == n {
			break
		}
		n = p
	}
	out := make([]string, len(rev))
	for i, c := range rev {
		out[len(rev)-1-i] = c
	}
	return out
}

// unguardedReach returns the components the walk reached along at least
// one chain that crosses no while: guard.
func unguardedReach(g *Graph, m *model.Model, root string, parent map[impactNode]impactNode) map[string]bool {
	free := map[string]bool{root: true}
	for changed := true; changed; {
		changed = false
		for n, p := range parent {
			if n == p || free[n.comp] || !free[p.comp] {
				continue
			}
			if guard(m, p.comp, n.comp) == "" {
				free[n.comp] = true
				changed = true
			}
		}
	}
	return free
}

// conditionsFor lists, for a component reached only through guards, the
// guards on the edges into the first guarded component on its path: the
// failure gets there, and so on to this component, while one holds.
func conditionsFor(m *model.Model, path []string, free map[string]bool, reached map[string]*Affected, root string) []string {
	gate := ""
	for _, c := range path {
		if !free[c] {
			gate = c
			break
		}
	}
	var out []string
	if dep := m.Components[gate]; dep != nil {
		for _, d := range dep.Depends {
			if d.WhileRaw == "" {
				continue
			}
			for _, child := range d.On {
				if child == root || reached[child] != nil {
					out = append(out, fmt.Sprintf("%s depends on %s while %s", gate, child, d.WhileRaw))
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// guard returns the while: on dependent's dependency on child, or "".
func guard(m *model.Model, child, dependent string) string {
	if m == nil {
		return ""
	}
	comp := m.Components[dependent]
	if comp == nil {
		return ""
	}
	for _, dep := range comp.Depends {
		if containsName(dep.On, child) {
			return dep.WhileRaw
		}
	}
	return ""
}
