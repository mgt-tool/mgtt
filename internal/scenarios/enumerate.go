// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import (
	"sort"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// Enumerate emits every plausible failure-chain scenario for the model:
// the chains through its enumeration graph (see BuildGraph, Expand).
// Output is deterministic; IDs are s-0001, s-0002, ... by sort position.
func Enumerate(m *model.Model, reg *providersupport.Registry) []Scenario {
	return Expand(BuildGraph(m, reg))
}

func sortedComponentNames(m *model.Model) []string {
	compNames := make([]string, 0, len(m.Components))
	for name := range m.Components {
		compNames = append(compNames, name)
	}
	sort.Strings(compNames)
	return compNames
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

// dependencyClosure maps each component to every component it depends on,
// directly or through others.
func dependencyClosure(m *model.Model) map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(m.Components))
	var visit func(string) map[string]bool
	visit = func(name string) map[string]bool {
		if got, ok := out[name]; ok {
			return got
		}
		set := map[string]bool{}
		out[name] = set // cycles are rejected by validate; this stops a loop regardless
		if comp := m.Components[name]; comp != nil {
			for _, dep := range comp.Depends {
				for _, on := range dep.On {
					set[on] = true
					for k := range visit(on) {
						set[k] = true
					}
				}
			}
		}
		return set
	}
	for name := range m.Components {
		visit(name)
	}
	return out
}

func containsName(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
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
