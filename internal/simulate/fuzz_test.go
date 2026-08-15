// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"testing"

	"github.com/mgt-tool/mgtt/internal/scenarios"
)

func TestFuzz_FixedSeedStable(t *testing.T) {
	m, reg := twoCompModel(t)
	scs := []scenarios.Scenario{
		{
			ID:   "s-1",
			Root: scenarios.RootRef{Component: "db", State: "down"},
			Chain: []scenarios.Step{
				{Component: "db", State: "down", EmitsOnEdge: "db_down"},
				{Component: "web", State: "down", Observes: []string{"status"}},
			},
		},
	}

	p1, f1, _ := Fuzz(m, reg, scs, 5, 42)
	p2, f2, _ := Fuzz(m, reg, scs, 5, 42)
	if p1 != p2 || f1 != f2 {
		t.Fatalf("fuzz unstable under fixed seed: (%d,%d) vs (%d,%d)", p1, f1, p2, f2)
	}
	if p1 == 0 {
		t.Errorf("want at least 1 pass over 5 iterations; got %d", p1)
	}
}

func TestFuzz_EmptyScenarioList(t *testing.T) {
	m, reg := twoCompModel(t)
	p, f, details := Fuzz(m, reg, nil, 3, 1)
	if p != 0 || f != 0 {
		t.Errorf("want 0/0 for empty list; got %d/%d", p, f)
	}
	if len(details) != 1 {
		t.Errorf("want 1 detail line; got %d: %v", len(details), details)
	}
}
