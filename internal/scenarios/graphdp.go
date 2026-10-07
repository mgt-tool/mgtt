// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
)

// Diagnosis asks of the chains only how many are live, how many touch each
// component, and which live ones are shortest. All three can be read off
// the graph without listing the chains, whose number grows exponentially
// with the model's depth: liveness is decided per node and per edge, and
// counting is dynamic programming over the live subgraph.
//
// The chains counted are exactly Expand's: from each root, every path
// along the failure graph that ends at a component with facts to observe,
// and the root alone when nothing else follows from it.

// Liveness says which nodes and edges the facts allow. Node is a
// component in a state; Edge is a failure passing from a component to one
// that depends on it. A nil func allows everything.
type Liveness struct {
	Node func(component, state string) bool
	Edge func(from, to string) bool
}

// Counts are what DP over the live graph yields.
type Counts struct {
	Live  int            // live chains
	Touch map[string]int // live chains through each component
}

// ErrCycle reports a failure graph with a cycle, which DP cannot count;
// callers enumerate instead.
var ErrCycle = errors.New("scenarios: the failure graph has a cycle")

type dpEdge struct {
	to    string
	st    GraphState
	label string
}

type dpKey struct{ comp, state string }

type dp struct {
	g    *Graph
	live Liveness
	root string
	suf  map[dpKey]int // live chain suffixes starting at a node, for root
	busy map[dpKey]bool
	err  error
}

func (l Liveness) node(c, s string) bool { return l.Node == nil || l.Node(c, s) }
func (l Liveness) edge(a, b string) bool { return l.Edge == nil || l.Edge(a, b) }

// next lists where a failure at comp in st goes on toward root's
// dependents, as Expand walks it.
func (g *Graph) next(root, comp string, st GraphState) []dpEdge {
	gc := g.Components[comp]
	if gc == nil {
		return nil
	}
	var out []dpEdge
	for _, d := range (walk{g: g, root: root}).carriedTo(gc) {
		gd := g.Components[d]
		if gd == nil || gd.Unresolved {
			continue
		}
		for _, ds := range gd.States {
			if m := matchEdgeLabel(st.Emits, ds.TriggeredBy); m != "" {
				out = append(out, dpEdge{to: d, st: ds, label: m})
			}
		}
	}
	return out
}

// suffixes counts the live chain suffixes that start at comp in st: the
// chain may end here when comp has facts to observe, or go on.
func (p *dp) suffixes(comp string, st GraphState) int {
	k := dpKey{comp, st.Name}
	if n, ok := p.suf[k]; ok {
		return n
	}
	if p.busy[k] {
		p.err = ErrCycle
		return 0
	}
	p.busy[k] = true
	n := 0
	if len(p.g.Components[comp].Observes) > 0 {
		n = 1
	}
	for _, e := range p.g.next(p.root, comp, st) {
		if p.live.edge(comp, e.to) && p.live.node(e.to, e.st.Name) {
			n += p.suffixes(e.to, e.st)
		}
	}
	delete(p.busy, k)
	p.suf[k] = n
	return n
}

// fromRoot counts the live chains rooted at comp in st, and adds each
// component's share to touch.
func (g *Graph) fromRoot(comp string, st GraphState, live Liveness, touch map[string]int) (int, error) {
	p := &dp{g: g, live: live, root: comp, suf: map[dpKey]int{}, busy: map[dpKey]bool{}}
	total := 0
	if live.node(comp, st.Name) {
		for _, e := range g.next(comp, comp, st) {
			if live.edge(comp, e.to) && live.node(e.to, e.st.Name) {
				total += p.suffixes(e.to, e.st)
			}
		}
		if total == 0 && g.rootStandsAlone(comp, st) {
			total = 1
		}
	}
	if p.err != nil {
		return 0, p.err
	}
	if total == 0 {
		return 0, nil
	}
	touch[comp] += total
	// Through every other node: live prefixes from the root times live
	// suffixes from the node. The graph is acyclic, so a chain meets a
	// component at most once and nothing is counted twice.
	pre := map[dpKey]int{}
	var order []dpKey
	states := map[dpKey]GraphState{}
	var visit func(c string, s GraphState)
	seen := map[dpKey]bool{}
	visit = func(c string, s GraphState) {
		k := dpKey{c, s.Name}
		if seen[k] {
			return
		}
		seen[k] = true
		for _, e := range g.next(comp, c, s) {
			if live.edge(c, e.to) && live.node(e.to, e.st.Name) {
				visit(e.to, e.st)
			}
		}
		order = append(order, k)
		states[k] = s
	}
	visit(comp, st)
	rootKey := dpKey{comp, st.Name}
	pre[rootKey] = 1
	for i := len(order) - 1; i >= 0; i-- { // reverse post-order: topological
		k := order[i]
		for _, e := range g.next(comp, k.comp, states[k]) {
			if live.edge(k.comp, e.to) && live.node(e.to, e.st.Name) {
				pre[dpKey{e.to, e.st.Name}] += pre[k]
			}
		}
	}
	for _, k := range order {
		if k == rootKey {
			continue
		}
		touch[k.comp] += pre[k] * p.suffixes(k.comp, states[k])
	}
	return total, nil
}

// rootStandsAlone reports whether a root's only chain is the root itself:
// it has facts to observe and no chain goes on from it at all, live or not.
func (g *Graph) rootStandsAlone(comp string, st GraphState) bool {
	if len(g.Components[comp].Observes) == 0 {
		return false
	}
	p := &dp{g: g, root: comp, suf: map[dpKey]int{}, busy: map[dpKey]bool{}}
	for _, e := range g.next(comp, comp, st) {
		if p.suffixes(e.to, e.st) > 0 {
			return false
		}
	}
	return true
}

// roots are the chain roots in Expand's order.
func (g *Graph) roots() []dpKey {
	names := make([]string, 0, len(g.Components))
	for n := range g.Components {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []dpKey
	for _, n := range names {
		if gc := g.Components[n]; !gc.Unresolved {
			for _, st := range gc.States {
				out = append(out, dpKey{n, st.Name})
			}
		}
	}
	return out
}

func (g *Graph) state(comp, name string) GraphState {
	for _, st := range g.Components[comp].States {
		if st.Name == name {
			return st
		}
	}
	return GraphState{}
}

// Count returns how many chains are live and how many live chains pass
// through each component, in time linear in the graph per root.
func Count(g *Graph, live Liveness) (Counts, error) {
	c := Counts{Touch: map[string]int{}}
	for _, r := range g.roots() {
		n, err := g.fromRoot(r.comp, g.state(r.comp, r.state), live, c.Touch)
		if err != nil {
			return Counts{}, err
		}
		c.Live += n
	}
	return c, nil
}

// LiveByLength calls f with each layer of live chains, shortest first,
// every chain built as Expand builds it and named by its content, until f
// returns false or the chains run out. Only the layers asked for are built.
func LiveByLength(g *Graph, live Liveness, f func(layer []Scenario) bool) {
	type partial struct {
		root  dpKey
		steps []Step
		at    GraphState
	}
	var frontier []partial
	var layer []Scenario
	for _, r := range g.roots() {
		st := g.state(r.comp, r.state)
		if !live.node(r.comp, r.state) {
			continue
		}
		frontier = append(frontier, partial{root: r, steps: []Step{{Component: r.comp, State: r.state}}, at: st})
		if g.rootStandsAlone(r.comp, st) {
			layer = append(layer, Scenario{Root: RootRef{Component: r.comp, State: r.state},
				Chain: []Step{{Component: r.comp, State: r.state, Observes: g.Components[r.comp].Observes}}})
		}
	}
	for len(frontier) > 0 || len(layer) > 0 {
		if len(layer) > 0 {
			sortScenarios(layer)
			for i := range layer {
				layer[i].ID = IDOf(layer[i].Chain)
			}
			if !f(layer) {
				return
			}
		}
		layer = nil
		var next []partial
		for _, p := range frontier {
			last := p.steps[len(p.steps)-1]
			for _, e := range g.next(p.root.comp, last.Component, p.at) {
				if !live.edge(last.Component, e.to) || !live.node(e.to, e.st.Name) {
					continue
				}
				steps := append(append([]Step(nil), p.steps...), Step{Component: e.to, State: e.st.Name})
				steps[len(steps)-2].EmitsOnEdge = e.label
				if obs := g.Components[e.to].Observes; len(obs) > 0 {
					chain := append([]Step(nil), steps...)
					chain[len(chain)-1].Observes = obs
					layer = append(layer, Scenario{Root: RootRef{Component: p.root.comp, State: p.root.state}, Chain: chain})
				}
				next = append(next, partial{root: p.root, steps: steps, at: e.st})
			}
		}
		frontier = next
	}
}

// IDOf is a chain's content-addressed ID, as Expand assigns it.
func IDOf(chain []Step) string {
	sum := sha256.Sum256([]byte(chainKey(chain)))
	return "s-" + hex.EncodeToString(sum[:])[:12]
}
