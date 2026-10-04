// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

// RootCause is the one rule both engines use to name a root cause, so
// simulate (engine.Plan) and diagnose (the strategies) cannot disagree.
//
// Candidates are the components in play (reachable over active edges) that
// facts show broken, less those a redundancy group still covers. A
// candidate is a casualty, not a cause, when another candidate could have
// broken it: one among its dependencies, reached through components not
// proven healthy, over active edges, and not through a group that holds.
// Of the candidates left, the most upstream -- the longest dependency
// chain from the entry -- wins, then model order. "" when nothing in play
// is seen broken.
func RootCause(in Input, entry string) string {
	if in.Model == nil || in.Registry == nil || in.Store == nil || entry == "" {
		return ""
	}
	reachable := bfsWalk(in, entry)
	inPlay := make(map[string]bool, len(reachable))
	for _, n := range reachable {
		inPlay[n] = true
	}
	states := deriveComponentStates(in)
	vars := in.Model.VarLookup(in.Registry)
	verdict := map[string]Verdict{}
	verdictOf := func(n string) Verdict {
		v, ok := verdict[n]
		if !ok {
			v = ComponentVerdict(in.Model, in.Registry, in.Store, n)
			verdict[n] = v
		}
		return v
	}
	candidate := map[string]bool{}
	for _, n := range reachable {
		if verdictOf(n) == Unhealthy && !RedundancyCovered(in.Model, in.Registry, in.Store, n) {
			candidate[n] = true
		}
	}
	if len(candidate) == 0 {
		return ""
	}

	// activeDeps lists n's dependencies over edges that are active and
	// not through a group that still holds.
	activeDeps := func(n string) []string {
		comp := in.Model.Components[n]
		if comp == nil {
			return nil
		}
		var out []string
		for _, dep := range comp.Depends {
			if !whileGuardActive(dep, n, in.Store, vars, states) {
				continue
			}
			for _, target := range dep.On {
				if held, _ := SatisfiedGroup(in.Model, in.Registry, in.Store, n, target); held || !inPlay[target] {
					continue
				}
				out = append(out, target)
			}
		}
		return out
	}

	// casualty reports whether another candidate could have broken x.
	casualty := func(x string) bool {
		seen := map[string]bool{x: true}
		stack := activeDeps(x)
		for len(stack) > 0 {
			y := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[y] {
				continue
			}
			seen[y] = true
			if candidate[y] {
				return true
			}
			if verdictOf(y) == Healthy {
				continue // a failure cannot pass through it
			}
			stack = append(stack, activeDeps(y)...)
		}
		return false
	}

	// depth is the longest dependency chain from the entry, over active
	// edges, to each component in play.
	depth := map[string]int{entry: 0}
	for changed := true; changed; {
		changed = false
		for _, n := range reachable {
			d, ok := depth[n]
			if !ok {
				continue
			}
			for _, t := range activeDeps(n) {
				if cur, seen := depth[t]; !seen || d+1 > cur {
					if d+1 > len(reachable) {
						continue // a cycle; validate rejects them, stop regardless
					}
					depth[t] = d + 1
					changed = true
				}
			}
		}
	}

	deepest := func(keep func(string) bool) string {
		best := ""
		for _, n := range in.Model.Order {
			if candidate[n] && keep(n) && (best == "" || depth[n] > depth[best]) {
				best = n
			}
		}
		return best
	}
	if best := deepest(func(n string) bool { return !casualty(n) }); best != "" {
		return best
	}
	// Every candidate could have been broken by another: only a cycle
	// does that. Name the deepest rather than nothing.
	return deepest(func(string) bool { return true })
}
