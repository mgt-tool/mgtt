// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import (
	"fmt"
	"sort"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// Enumerate walks the model + type registry and emits every plausible
// failure-chain scenario. Output is deterministic and IDs assigned
// s-0001, s-0002, ... by sort position.
//
// Permissive default: when a downstream state has no TriggeredBy
// declaration, it accepts any upstream can_cause label.
func Enumerate(m *model.Model, reg *providersupport.Registry) []Scenario {
	// Precompute reverse adjacency: for each component, which components
	// list it as a dep target. Avoids the N×N rescan extendChain used to
	// do for every recursive call — each chain extension is now O(1)
	// lookup instead of O(|components|).
	inverse := inverseDeps(m)

	var out []Scenario
	for _, compName := range sortedComponentNames(m) {
		out = append(out, scenariosRootedAt(m, reg, inverse, compName)...)
	}

	sortScenarios(out)
	for i := range out {
		out[i].ID = fmt.Sprintf("s-%04d", i+1)
	}
	return out
}

func sortedComponentNames(m *model.Model) []string {
	compNames := make([]string, 0, len(m.Components))
	for name := range m.Components {
		compNames = append(compNames, name)
	}
	sort.Strings(compNames)
	return compNames
}

// scenariosRootedAt emits every chain rooted at compName across all of
// its non-default states.
func scenariosRootedAt(m *model.Model, reg *providersupport.Registry, inverse map[string][]string, compName string) []Scenario {
	comp := m.Components[compName]
	t, _, err := comp.ResolveType(m, reg)
	if err != nil || t == nil {
		return nil
	}
	var out []Scenario
	for _, state := range t.States {
		if state.Name == t.DefaultActiveState {
			continue
		}
		for _, chain := range extendChain(m, reg, inverse, compName, state, map[string]bool{}) {
			out = append(out, Scenario{
				Root:  RootRef{Component: compName, State: state.Name},
				Chain: chain,
			})
		}
	}
	return out
}

func sortScenarios(out []Scenario) {
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Length() != out[j].Length() {
			return out[i].Length() < out[j].Length()
		}
		if out[i].Root.Component != out[j].Root.Component {
			return out[i].Root.Component < out[j].Root.Component
		}
		if out[i].Root.State != out[j].Root.State {
			return out[i].Root.State < out[j].Root.State
		}
		return chainKey(out[i].Chain) < chainKey(out[j].Chain)
	})
}

// inverseDeps returns the reverse adjacency: component → sorted list of
// components that declare it in their Depends.On. Built once per
// Enumerate so extendChain's recursion doesn't re-walk every component's
// Depends list at every step.
func inverseDeps(m *model.Model) map[string][]string {
	rev := map[string]map[string]bool{}
	for name, comp := range m.Components {
		for _, dep := range comp.Depends {
			for _, on := range dep.On {
				if rev[on] == nil {
					rev[on] = map[string]bool{}
				}
				rev[on][name] = true
			}
		}
	}
	out := make(map[string][]string, len(rev))
	for on, set := range rev {
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out[on] = keys
	}
	return out
}

func extendChain(m *model.Model, reg *providersupport.Registry, inverse map[string][]string, compName string, state providersupport.StateDef, visited map[string]bool) [][]Step {
	if visited[compName] {
		return nil
	}
	visited[compName] = true
	defer delete(visited, compName)

	comp := m.Components[compName]
	if comp == nil {
		return nil
	}
	t, _, err := comp.ResolveType(m, reg)
	if err != nil || t == nil {
		return nil
	}
	downstreams := inverse[compName]
	if len(downstreams) == 0 {
		return terminalChain(compName, state, t)
	}
	return extendAcrossDownstreams(m, reg, inverse, compName, state, t.FailureModes[state.Name], downstreams, visited)
}

// terminalChain returns a single-step chain observing every fact on t
// when t has any, nil otherwise. Used when the component has no
// downstream components to propagate to.
func terminalChain(compName string, state providersupport.StateDef, t *providersupport.Type) [][]Step {
	if len(t.Facts) == 0 {
		return nil
	}
	return [][]Step{{{Component: compName, State: state.Name, Observes: factNames(t.Facts)}}}
}

// extendAcrossDownstreams walks each downstream candidate and composes
// chains rooted at (compName, state) with every valid downstream link.
func extendAcrossDownstreams(m *model.Model, reg *providersupport.Registry, inverse map[string][]string, compName string, state providersupport.StateDef, emits []string, downstreams []string, visited map[string]bool) [][]Step {
	var out [][]Step
	for _, dname := range downstreams {
		dcomp := m.Components[dname]
		if dcomp == nil {
			continue
		}
		dt, _, err := dcomp.ResolveType(m, reg)
		if err != nil || dt == nil {
			continue
		}
		out = append(out, chainsThroughDownstream(m, reg, inverse, compName, state, emits, dname, dt, visited)...)
	}
	return out
}

// chainsThroughDownstream emits every chain that extends (compName,
// state) through dname, for each non-default dstate of dt whose
// TriggeredBy matches one of emits.
func chainsThroughDownstream(m *model.Model, reg *providersupport.Registry, inverse map[string][]string, compName string, state providersupport.StateDef, emits []string, dname string, dt *providersupport.Type, visited map[string]bool) [][]Step {
	var out [][]Step
	head := Step{Component: compName, State: state.Name}
	for _, dstate := range dt.States {
		if dstate.Name == dt.DefaultActiveState {
			continue
		}
		match := matchEdgeLabel(emits, dstate.TriggeredBy)
		if match == "" {
			continue
		}
		head.EmitsOnEdge = match
		for _, suffix := range extendChain(m, reg, inverse, dname, dstate, visited) {
			out = append(out, append([]Step{head}, suffix...))
		}
		// Also emit a 2-step chain terminating at dname — a real
		// incident may stop at dname's symptom layer even when
		// deeper chains exist downstream.
		if len(dt.Facts) > 0 {
			out = append(out, []Step{head, {Component: dname, State: dstate.Name, Observes: factNames(dt.Facts)}})
		}
	}
	return out
}

// matchEdgeLabel returns the first label in emits that the downstream
// state's TriggeredBy accepts. Empty TriggeredBy means "accept any"
// (permissive default), so emits[0] wins in that case.
func matchEdgeLabel(emits, triggeredBy []string) string {
	if len(triggeredBy) == 0 {
		if len(emits) > 0 {
			return emits[0]
		}
		return ""
	}
	for _, e := range emits {
		for _, tb := range triggeredBy {
			if e == tb {
				return e
			}
		}
	}
	return ""
}

func factNames(f map[string]*providersupport.FactSpec) []string {
	out := make([]string, 0, len(f))
	for k := range f {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func chainKey(chain []Step) string {
	s := ""
	for _, step := range chain {
		s += step.Component + ":" + step.State + ">"
	}
	return s
}
