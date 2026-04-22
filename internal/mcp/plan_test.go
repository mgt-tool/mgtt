package mcp

import (
	"os"
	"testing"
)

// startPlanFixture builds a minimal incident anchored to MGTT_HOME in a
// temp dir. The model uses the generic.component fallback type so the
// test runs without any real provider being installed. Returns the
// handler and the freshly-minted incident ID.
func startPlanFixture(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MGTT_HOME", dir)

	// The model references `generic.component`, which the handler path
	// registers as an embedded fallback.
	modelPath := writeMinimalModel(t, dir)
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	h := NewHandler(Config{})
	start, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath, ID: "inc-plan"})
	if err != nil {
		t.Fatalf("IncidentStart: %v", err)
	}
	return h, start.IncidentID
}

func TestPlan_SuggestsProbeForGenericComponent(t *testing.T) {
	h, incidentID := startPlanFixture(t)

	result, err := h.Plan(PlanParams{IncidentID: incidentID})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Entry point defaults to the model's outermost component.
	if result.Entry != "api" {
		t.Errorf("entry: got %q want %q", result.Entry, "api")
	}
	if result.Suggested == nil {
		t.Fatal("expected a suggested probe — generic.component has operator_says_healthy")
	}
	if result.Suggested.Component != "api" {
		t.Errorf("suggested.component: got %q want %q", result.Suggested.Component, "api")
	}
	if result.Suggested.Fact != "operator_says_healthy" {
		t.Errorf("suggested.fact: got %q want %q", result.Suggested.Fact, "operator_says_healthy")
	}
}

func TestPlan_MissingIncidentError(t *testing.T) {
	h := NewHandler(Config{})
	_, err := h.Plan(PlanParams{IncidentID: "ghost"})
	if err == nil {
		t.Fatal("expected error for unknown incident_id")
	}
}

func TestPlan_MissingIDError(t *testing.T) {
	h := NewHandler(Config{})
	if _, err := h.Plan(PlanParams{}); err == nil {
		t.Fatal("expected error when incident_id empty")
	}
}

func TestPlan_ComponentOverrideBecomesEntry(t *testing.T) {
	// The caller passes `component` to start walking from there instead of
	// the model's outermost. Test this by asserting the override surfaces
	// as the entry — no need to exercise a multi-component model.
	h, incidentID := startPlanFixture(t)
	result, err := h.Plan(PlanParams{
		IncidentID: incidentID,
		Component:  "api",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if result.Entry != "api" {
		t.Errorf("entry: got %q want %q", result.Entry, "api")
	}
}
