// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"fmt"
	"testing"

	"github.com/mgt-tool/mgtt/internal/engine"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// chainOf builds a chain from component/state pairs.
func chainOf(id string, steps ...string) scenarios.Scenario {
	s := scenarios.Scenario{ID: id}
	for i := 0; i+1 < len(steps); i += 2 {
		s.Chain = append(s.Chain, scenarios.Step{Component: steps[i], State: steps[i+1]})
	}
	s.Root = scenarios.RootRef{Component: s.Chain[0].Component, State: s.Chain[0].State}
	return s
}

func TestListScenarios_RepresentativesByDefault(t *testing.T) {
	scs := []scenarios.Scenario{
		chainOf("s-1", "db", "down", "api", "crashed", "edge", "degraded"),
		chainOf("s-2", "db", "down", "edge", "degraded"),
		chainOf("s-3", "cache", "down", "api", "crashed"),
		chainOf("s-4", "db", "down", "api", "degraded", "edge", "degraded"),
	}
	got, err := listScenarios(scs, ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Chains != 4 || got.Total != 2 || got.NextPageToken != "" {
		t.Fatalf("chains %d, total %d, next %q; want 4 chains in 2 classes on one page", got.Chains, got.Total, got.NextPageToken)
	}
	if got.Scenarios[0].ID != "s-2" || got.Scenarios[0].Count != 3 || got.Scenarios[1].Count != 1 {
		t.Errorf("got %+v, want s-2 standing for 3, then cache's 1", got.Scenarios)
	}

	all, err := listScenarios(scs, ListParams{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 4 || len(all.Scenarios) != 4 || all.Scenarios[0].Count != 0 {
		t.Errorf("all: got total %d, %d entries, count %d; want every chain, uncounted", all.Total, len(all.Scenarios), all.Scenarios[0].Count)
	}
}

func TestListScenarios_PagesThroughEveryEntryOnce(t *testing.T) {
	var scs []scenarios.Scenario
	for i := 0; i < 120; i++ {
		scs = append(scs, chainOf(fmt.Sprintf("s-%03d", i), fmt.Sprintf("c%03d", i), "down"))
	}
	seen := map[string]bool{}
	token, pages := "", 0
	for {
		got, err := listScenarios(scs, ListParams{PageToken: token})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if len(got.Scenarios) > defaultPageSize {
			t.Fatalf("page %d holds %d entries, over the default %d", pages, len(got.Scenarios), defaultPageSize)
		}
		for _, s := range got.Scenarios {
			if seen[s.ID] {
				t.Fatalf("%s listed twice", s.ID)
			}
			seen[s.ID] = true
		}
		if token = got.NextPageToken; token == "" {
			break
		}
	}
	if pages != 3 || len(seen) != 120 {
		t.Errorf("got %d pages covering %d entries, want 3 covering 120", pages, len(seen))
	}

	big, _ := listScenarios(scs, ListParams{Limit: 100000})
	if len(big.Scenarios) != 120 || big.NextPageToken != "" {
		t.Errorf("a limit over the cap: got %d entries, next %q; want all 120 under the %d cap", len(big.Scenarios), big.NextPageToken, maxPageSize)
	}
}

func TestListScenarios_RejectsATokenItDidNotIssue(t *testing.T) {
	scs := []scenarios.Scenario{chainOf("s-1", "db", "down")}
	for _, tok := range []string{"abc", "-1", "2"} {
		if _, err := listScenarios(scs, ListParams{PageToken: tok}); err == nil {
			t.Errorf("page_token %q: want an error", tok)
		}
	}
}

func TestIncidentSnapshot_CountsWhatItsListsStandFor(t *testing.T) {
	h, incidentID := startPlanFixture(t)
	snap, err := h.IncidentSnapshot(IncidentSnapshotParams{IncidentID: incidentID})
	if err != nil {
		t.Fatal(err)
	}
	listed := 0
	for _, s := range snap.SurvivingScenarios {
		listed += s.Count
	}
	if snap.SurvivingChains == 0 || listed != snap.SurvivingChains || snap.SurvivingClasses != len(snap.SurvivingScenarios) {
		t.Errorf("surviving: %d chains, %d classes, %d listed standing for %d; want the counts to add up",
			snap.SurvivingChains, snap.SurvivingClasses, len(snap.SurvivingScenarios), listed)
	}
}

func TestSuggestedProbe_NamesAFewAndCountsAll(t *testing.T) {
	p := &engine.Probe{Component: "db", Fact: "available"}
	for i := 0; i < 25; i++ {
		p.Eliminates = append(p.Eliminates, fmt.Sprintf("s-%04d", i))
	}
	got := toSuggested(p, nil)
	if len(got.Eliminates) != eliminatesShown || got.EliminatesCount != 25 {
		t.Errorf("got %d named, count %d; want %d named, count 25", len(got.Eliminates), got.EliminatesCount, eliminatesShown)
	}
}
