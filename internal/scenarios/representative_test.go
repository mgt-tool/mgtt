// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import "testing"

func chain(id string, steps ...string) Scenario {
	s := Scenario{ID: id}
	for i := 0; i+1 < len(steps); i += 2 {
		s.Chain = append(s.Chain, Step{Component: steps[i], State: steps[i+1]})
	}
	s.Root = RootRef{Component: s.Chain[0].Component, State: s.Chain[0].State}
	return s
}

func TestRepresentatives_OnePerRootStateAndTerminal(t *testing.T) {
	scs := []Scenario{
		chain("s-1", "db", "down", "api", "crashed", "edge", "degraded"),
		chain("s-2", "db", "down", "edge", "degraded"),                      // same class, shorter
		chain("s-3", "db", "down", "api", "crashed"),                        // ends at api: its own class
		chain("s-4", "db", "slow", "api", "degraded", "edge", "degraded"),   // another root state
		chain("s-5", "db", "down", "api", "degraded", "edge", "degraded"),   // same class as s-1, as long
		chain("s-6", "cache", "down", "api", "crashed", "edge", "degraded"), // another root
	}
	got := Representatives(scs)
	want := []struct {
		id    string
		count int
	}{{"s-2", 3}, {"s-3", 1}, {"s-4", 1}, {"s-6", 1}}
	if len(got) != len(want) {
		t.Fatalf("got %d classes, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].ID != w.id || got[i].Count != w.count {
			t.Errorf("class %d: got %s ×%d, want %s ×%d", i, got[i].ID, got[i].Count, w.id, w.count)
		}
	}
	total := 0
	for _, r := range got {
		total += r.Count
	}
	if total != len(scs) {
		t.Errorf("counts sum to %d, want every chain once (%d)", total, len(scs))
	}
}

func TestRepresentatives_TiesKeepTheFirst(t *testing.T) {
	got := Representatives([]Scenario{
		chain("s-1", "db", "down", "api", "crashed", "edge", "degraded"),
		chain("s-2", "db", "down", "api", "degraded", "edge", "degraded"),
	})
	if len(got) != 1 || got[0].ID != "s-1" || got[0].Count != 2 {
		t.Fatalf("got %+v, want s-1 standing for 2", got)
	}
}
