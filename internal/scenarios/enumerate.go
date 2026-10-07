// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	sort.SliceStable(out, func(i, j int) bool { return Less(out[i], out[j]) })
}

// Less is scenario order: shorter chains first, then by root, root state
// and the chain's steps. It is the order listings use and the order a
// strategy breaks ties by, so ties never depend on an ID.
func Less(a, b Scenario) bool {
	if a.Length() != b.Length() {
		return a.Length() < b.Length()
	}
	if a.Root.Component != b.Root.Component {
		return a.Root.Component < b.Root.Component
	}
	if a.Root.State != b.Root.State {
		return a.Root.State < b.Root.State
	}
	return chainKey(a.Chain) < chainKey(b.Chain)
}

// assignIDs names each chain by its content: s- and the first 12 hex digits
// of the SHA-256 of its steps. An ID survives any change to the model that
// leaves the chain itself alone, so incident snapshots and eliminated lists
// compare across model revisions. Should two chains ever share a prefix,
// the later one in scenario order takes a numbered suffix.
func assignIDs(out []Scenario) {
	seen := map[string]int{}
	for i := range out {
		sum := sha256.Sum256([]byte(chainKey(out[i].Chain)))
		id := "s-" + hex.EncodeToString(sum[:])[:12]
		if n := seen[id]; n > 0 {
			seen[id] = n + 1
			id = fmt.Sprintf("%s-%d", id, n+1)
		} else {
			seen[id] = 1
		}
		out[i].ID = id
	}
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
