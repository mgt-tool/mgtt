// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import "sort"

// Classes are Representatives over the graph: for the live chains and for
// the eliminated ones, one representative per class with its count, as
// Representatives returns them from Expand's chains split by live, without
// listing a chain.
//
// Per root, a walk in topological order carries each node twice, once for
// paths with nothing dead on them and once for paths with something dead,
// with how many paths reach it and the least of them in scenario order.
// The least path is well defined node by node: a chain's prefix is the
// least path to where it stops, or a lesser prefix would make a lesser
// chain. A cycle returns ErrCycle, and callers list instead.
func Classes(g *Graph, live Liveness) (alive, dead []Representative, err error) {
	for _, r := range g.roots() {
		a, d, err := g.rootClasses(r, live)
		if err != nil {
			return nil, nil, err
		}
		alive, dead = append(alive, a...), append(dead, d...)
	}
	byOrder := func(rs []Representative) {
		sort.SliceStable(rs, func(i, j int) bool { return Less(rs[i].Scenario, rs[j].Scenario) })
		for i := range rs {
			rs[i].ID = IDOf(rs[i].Chain)
		}
	}
	byOrder(alive)
	byOrder(dead)
	return alive, dead, nil
}

// classNode is a component in a state, reached with (dead) or without
// something contradicted on the way.
type classNode struct {
	comp, state string
	dead        bool
}

type classPath struct {
	count int
	steps []Step // the least path here, in scenario order
	at    GraphState
}

func (g *Graph) rootClasses(r dpKey, live Liveness) (alive, dead []Representative, err error) {
	rootSt := g.state(r.comp, r.state)
	start := classNode{r.comp, r.state, !live.node(r.comp, r.state)}
	paths := map[classNode]*classPath{start: {count: 1, steps: []Step{{Component: r.comp, State: r.state}}, at: rootSt}}

	// Topological order of what the root reaches, by depth-first post-order.
	var order []classNode
	seen, busy := map[classNode]bool{}, map[classNode]bool{}
	var visit func(n classNode, st GraphState)
	visit = func(n classNode, st GraphState) {
		if busy[n] {
			err = ErrCycle
			return
		}
		if seen[n] {
			return
		}
		seen[n], busy[n] = true, true
		for _, e := range g.next(r.comp, n.comp, st) {
			visit(g.step(n, e, live), e.st)
		}
		delete(busy, n)
		order = append(order, n)
	}
	visit(start, rootSt)
	if err != nil {
		return nil, nil, err
	}

	type class struct {
		terminal string
		dead     bool
	}
	reps := map[class]*Representative{}
	add := func(k class, steps []Step, n int) {
		rep := reps[k]
		cand := Scenario{Root: RootRef{Component: r.comp, State: r.state}, Chain: steps}
		if rep == nil {
			reps[k] = &Representative{Scenario: cand, Count: n}
			return
		}
		rep.Count += n
		if Less(cand, rep.Scenario) {
			rep.Scenario = cand
		}
	}
	for i := len(order) - 1; i >= 0; i-- { // reverse post-order: topological
		n := order[i]
		p := paths[n]
		if p == nil {
			continue
		}
		if n != start && len(g.Components[n.comp].Observes) > 0 {
			add(class{n.comp, n.dead}, finish(p.steps, g.Components[n.comp].Observes), p.count)
		}
		for _, e := range g.next(r.comp, n.comp, p.at) {
			to := g.step(n, e, live)
			steps := append(append([]Step(nil), p.steps...), Step{Component: e.to, State: e.st.Name})
			steps[len(steps)-2].EmitsOnEdge = e.label
			q := paths[to]
			if q == nil {
				paths[to] = &classPath{count: p.count, steps: steps, at: e.st}
				continue
			}
			q.count += p.count
			if len(steps) < len(q.steps) || len(steps) == len(q.steps) && chainKey(steps) < chainKey(q.steps) {
				q.steps = steps
			}
		}
	}
	if g.rootStandsAlone(r.comp, rootSt) {
		add(class{r.comp, start.dead}, finish(paths[start].steps, g.Components[r.comp].Observes), 1)
	}
	for k, rep := range reps {
		if k.dead {
			dead = append(dead, *rep)
		} else {
			alive = append(alive, *rep)
		}
	}
	return alive, dead, nil
}

// step is where edge e from n leads: dead once anything on the way is.
func (g *Graph) step(n classNode, e dpEdge, live Liveness) classNode {
	return classNode{e.to, e.st.Name, n.dead || !live.edge(n.comp, e.to) || !live.node(e.to, e.st.Name)}
}

// finish is a chain ending at its last step, which carries the facts to
// observe, as Expand builds it.
func finish(steps []Step, observes []string) []Step {
	out := append([]Step(nil), steps...)
	out[len(out)-1].Observes = observes
	return out
}
