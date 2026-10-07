// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// randomGraph is a random layered failure graph: some states emit labels,
// some dependents answer to some of them, some components observe facts,
// some links carry a failure only for some roots, as a redundancy group's do.
func randomGraph(rng *rand.Rand) *Graph {
	layers, width := 2+rng.IntN(3), 1+rng.IntN(3)
	labels := []string{"a", "b"}
	g := &Graph{Components: map[string]*GraphComponent{}}
	var names [][]string
	for l := 0; l < layers; l++ {
		var row []string
		for w := 0; w < width; w++ {
			n := fmt.Sprintf("c%d%d", l, w)
			gc := &GraphComponent{}
			if rng.IntN(4) > 0 {
				gc.Observes = []string{"f"}
			}
			for s := 0; s < 1+rng.IntN(2); s++ {
				st := GraphState{Name: fmt.Sprintf("s%d", s)}
				if rng.IntN(3) > 0 {
					st.Emits = []string{labels[rng.IntN(2)]}
				}
				if rng.IntN(3) == 0 {
					st.TriggeredBy = []string{labels[rng.IntN(2)]}
				}
				gc.States = append(gc.States, st)
			}
			g.Components[n] = gc
			row = append(row, n)
		}
		names = append(names, row)
	}
	all := slices.Concat(names...)
	for l := 0; l+1 < layers; l++ {
		for _, n := range names[l] {
			for _, d := range names[l+1] {
				if rng.IntN(3) == 0 {
					continue
				}
				link := GraphLink{To: d}
				if rng.IntN(4) == 0 {
					link.Group = true
					for _, r := range all {
						if rng.IntN(2) == 0 {
							link.Roots = append(link.Roots, r)
						}
					}
				}
				g.Components[n].Dependents = append(g.Components[n].Dependents, link)
			}
		}
	}
	return g
}

func randomLiveness(rng *rand.Rand) Liveness {
	deadNode, deadEdge := map[string]bool{}, map[string]bool{}
	seed := rng.Uint64()
	return Liveness{
		Node: func(c, s string) bool {
			k := c + "/" + s
			if _, ok := deadNode[k]; !ok {
				deadNode[k] = (seed^uint64(len(k)*31+int(k[1])*7+int(k[len(k)-1])))%5 == 0
			}
			return !deadNode[k]
		},
		Edge: func(a, b string) bool {
			k := a + ">" + b
			if _, ok := deadEdge[k]; !ok {
				deadEdge[k] = (seed>>3^uint64(int(a[1])*13+int(b[2])*5))%6 == 0
			}
			return !deadEdge[k]
		},
	}
}

func liveChain(s Scenario, l Liveness) bool {
	for i, st := range s.Chain {
		if !l.node(st.Component, st.State) {
			return false
		}
		if i+1 < len(s.Chain) && !l.edge(st.Component, s.Chain[i+1].Component) {
			return false
		}
	}
	return true
}

// DP over the graph must agree with enumerating every chain: the same live
// count, the same per-component counts, the same chains layer by layer.
func TestGraphDP_EqualsEnumeration(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	for i := 0; i < 400; i++ {
		g := randomGraph(rng)
		for j := 0; j < 3; j++ {
			live := randomLiveness(rng)
			if j == 0 {
				live = Liveness{}
			}
			var want []Scenario
			touch := map[string]int{}
			for _, s := range Expand(g) {
				if liveChain(s, live) {
					want = append(want, s)
					seen := map[string]bool{}
					for _, st := range s.Chain {
						if !seen[st.Component] {
							seen[st.Component] = true
							touch[st.Component]++
						}
					}
				}
			}
			got, err := Count(g, live)
			if err != nil {
				t.Fatal(err)
			}
			if got.Live != len(want) {
				t.Fatalf("graph %d/%d: DP counts %d live chains, enumeration %d", i, j, got.Live, len(want))
			}
			for c, n := range touch {
				if got.Touch[c] != n {
					t.Fatalf("graph %d/%d: %s touched by %d per DP, %d per enumeration", i, j, c, got.Touch[c], n)
				}
			}
			var layered []Scenario
			LiveByLength(g, live, func(layer []Scenario) bool {
				layered = append(layered, layer...)
				return true
			})
			if len(layered) != len(want) {
				t.Fatalf("graph %d/%d: layers hold %d chains, enumeration %d", i, j, len(layered), len(want))
			}
			for k := range want {
				if want[k].ID != layered[k].ID || chainKey(want[k].Chain) != chainKey(layered[k].Chain) {
					t.Fatalf("graph %d/%d: chain %d is %s (%s) by layers, %s (%s) by enumeration", i, j, k, layered[k].ID, chainKey(layered[k].Chain), want[k].ID, chainKey(want[k].Chain))
				}
			}
		}
	}
}
