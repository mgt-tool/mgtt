// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

// Terminal is the component a chain ends at: where the failure is seen.
func (s Scenario) Terminal() string {
	if len(s.Chain) == 0 {
		return s.Root.Component
	}
	return s.Chain[len(s.Chain)-1].Component
}

// Representative stands for a class of chains that agree on root, root
// state and terminal component: one failure, seen at one place, by every
// route between them. It is the class's shortest chain (the first, in the
// order given, among equals), and Count is how many chains the class holds.
//
// Listings show these instead of chains: the storefront's thousands of
// chains are a few hundred classes, and what a reader wants from a listing
// is the classes. The engine still reasons over every chain.
type Representative struct {
	Scenario
	Count int
}

// Representatives returns one Representative per class in scs, in the order
// the classes first appear.
func Representatives(scs []Scenario) []Representative {
	type class struct{ root, state, terminal string }
	at := map[class]int{}
	var out []Representative
	for _, s := range scs {
		k := class{s.Root.Component, s.Root.State, s.Terminal()}
		i, seen := at[k]
		if !seen {
			at[k] = len(out)
			out = append(out, Representative{Scenario: s, Count: 1})
			continue
		}
		out[i].Count++
		if len(s.Chain) < len(out[i].Chain) {
			out[i].Scenario = s
		}
	}
	return out
}
