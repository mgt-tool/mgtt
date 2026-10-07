// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import (
	"math/rand/v2"
	"testing"
)

// Classes over the graph must equal Representatives over Expand's chains
// split by liveness: the same classes in the same order, each with the
// same count and the same representative chain, observes and edge labels
// included.
func TestClasses_EqualsRepresentatives(t *testing.T) {
	rng := rand.New(rand.NewPCG(13, 17))
	for i := 0; i < 400; i++ {
		g := randomGraph(rng)
		for j := 0; j < 3; j++ {
			live := randomLiveness(rng)
			if j == 0 {
				live = Liveness{}
			}
			var liveScs, deadScs []Scenario
			for _, s := range Expand(g) {
				if liveChain(s, live) {
					liveScs = append(liveScs, s)
				} else {
					deadScs = append(deadScs, s)
				}
			}
			alive, dead, err := Classes(g, live)
			if err != nil {
				t.Fatal(err)
			}
			same := func(side string, got, want []Representative) {
				if len(got) != len(want) {
					t.Fatalf("graph %d/%d %s: %d classes, want %d", i, j, side, len(got), len(want))
				}
				for k := range want {
					g, w := got[k], want[k]
					if g.Count != w.Count || g.ID != w.ID || g.Root != w.Root || !sameSteps(g.Chain, w.Chain) {
						t.Fatalf("graph %d/%d %s class %d:\n got %d × %s %+v\nwant %d × %s %+v", i, j, side, k, g.Count, g.ID, g.Chain, w.Count, w.ID, w.Chain)
					}
				}
			}
			same("alive", alive, Representatives(liveScs))
			same("dead", dead, Representatives(deadScs))
		}
	}
}

func sameSteps(a, b []Step) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Component != b[i].Component || a[i].State != b[i].State || a[i].EmitsOnEdge != b[i].EmitsOnEdge || len(a[i].Observes) != len(b[i].Observes) {
			return false
		}
	}
	return true
}

func TestClasses_CycleIsReported(t *testing.T) {
	st := []GraphState{{Name: "down", Emits: []string{"x"}}}
	g := &Graph{Components: map[string]*GraphComponent{
		"a": {States: st, Observes: []string{"f"}, Dependents: []GraphLink{{To: "b"}}},
		"b": {States: st, Observes: []string{"f"}, Dependents: []GraphLink{{To: "a"}}},
	}}
	if _, _, err := Classes(g, Liveness{}); err != ErrCycle {
		t.Fatalf("want ErrCycle, got %v", err)
	}
}
