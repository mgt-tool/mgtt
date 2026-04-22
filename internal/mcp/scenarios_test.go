package mcp

import (
	"testing"
)

func TestScenariosList_ReturnsEnumeratedChains(t *testing.T) {
	h, incidentID := startPlanFixture(t)

	result, err := h.ScenariosList(ScenariosListParams{IncidentID: incidentID})
	if err != nil {
		t.Fatalf("ScenariosList: %v", err)
	}
	// generic.component has one non-default state (stopped), so the
	// single-component model produces at least one enumerated chain.
	if len(result.Scenarios) == 0 {
		t.Fatal("expected at least one enumerated scenario for api.stopped")
	}
	for _, sc := range result.Scenarios {
		if sc.ID == "" {
			t.Error("scenario missing id")
		}
		if sc.Root.Component == "" {
			t.Error("scenario missing root.component")
		}
		if len(sc.Chain) == 0 {
			t.Error("scenario must have non-empty chain")
		}
	}
}

func TestScenariosList_MissingIncidentError(t *testing.T) {
	h := NewHandler(Config{})
	if _, err := h.ScenariosList(ScenariosListParams{IncidentID: "ghost"}); err == nil {
		t.Fatal("expected error for unknown incident")
	}
}

func TestScenariosAlive_FiltersByFacts(t *testing.T) {
	// With api.operator_says_healthy=true, no failure scenario is
	// consistent — api can't be in a "stopped" chain.
	h, incidentID := startPlanFixture(t)

	// Sanity: baseline has scenarios.
	before, err := h.ScenariosList(ScenariosListParams{IncidentID: incidentID})
	if err != nil {
		t.Fatalf("ScenariosList: %v", err)
	}
	if len(before.Scenarios) == 0 {
		t.Fatal("test setup: expected enumerated scenarios")
	}

	if _, err := h.FactAdd(FactAddParams{
		IncidentID: incidentID,
		Component:  "api",
		Key:        "operator_says_healthy",
		Value:      true,
	}); err != nil {
		t.Fatalf("FactAdd: %v", err)
	}

	alive, err := h.ScenariosAlive(ScenariosAliveParams{IncidentID: incidentID})
	if err != nil {
		t.Fatalf("ScenariosAlive: %v", err)
	}
	if len(alive.Scenarios) != 0 {
		t.Errorf("operator_says_healthy=true must eliminate stopped-chain scenarios, got %d alive", len(alive.Scenarios))
	}
}

func TestScenariosAlive_NoFactsKeepsEverythingAlive(t *testing.T) {
	h, incidentID := startPlanFixture(t)
	before, _ := h.ScenariosList(ScenariosListParams{IncidentID: incidentID})
	alive, err := h.ScenariosAlive(ScenariosAliveParams{IncidentID: incidentID})
	if err != nil {
		t.Fatalf("ScenariosAlive: %v", err)
	}
	if len(alive.Scenarios) != len(before.Scenarios) {
		t.Errorf("with no facts, alive should equal full list: got %d alive / %d total",
			len(alive.Scenarios), len(before.Scenarios))
	}
}
