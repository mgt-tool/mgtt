package mcp

import (
	"encoding/json"
	"testing"
)

func TestIncidentSnapshot_EmptyIncidentHasShape(t *testing.T) {
	h, incidentID := startPlanFixture(t)

	snap, err := h.IncidentSnapshot(IncidentSnapshotParams{IncidentID: incidentID})
	if err != nil {
		t.Fatalf("IncidentSnapshot: %v", err)
	}

	if snap.IncidentID != incidentID {
		t.Errorf("incident_id: got %q want %q", snap.IncidentID, incidentID)
	}
	if snap.Status != "open" {
		t.Errorf("status: got %q want %q", snap.Status, "open")
	}
	if snap.ModelRef.Path == "" {
		t.Error("model_ref.path must be populated")
	}
	if snap.EntryPoint != "api" {
		t.Errorf("entry_point: got %q want %q", snap.EntryPoint, "api")
	}
	// No facts, no eliminated scenarios yet.
	if len(snap.Facts) != 0 {
		t.Errorf("facts: got %d want 0", len(snap.Facts))
	}
	if len(snap.EliminatedScenarios) != 0 {
		t.Errorf("eliminated: got %d want 0", len(snap.EliminatedScenarios))
	}
	// With no facts, the full enumerated set is alive.
	if len(snap.SurvivingScenarios) == 0 {
		t.Error("surviving: baseline must include the enumerated chains")
	}
	// Suggested next probe: generic.component's operator_says_healthy.
	if snap.SuggestedNext == nil {
		t.Error("suggested_next must be present when plan has a suggestion")
	}
}

func TestIncidentSnapshot_ClosedIncidentReflectsStatusAndVerdict(t *testing.T) {
	h, incidentID := startPlanFixture(t)
	if _, err := h.IncidentEnd(IncidentEndParams{
		IncidentID: incidentID,
		Verdict:    "api degraded — replica count low",
	}); err != nil {
		t.Fatalf("IncidentEnd: %v", err)
	}
	snap, err := h.IncidentSnapshot(IncidentSnapshotParams{IncidentID: incidentID})
	if err != nil {
		t.Fatalf("IncidentSnapshot: %v", err)
	}
	if snap.Status != "closed" {
		t.Errorf("status: got %q want %q", snap.Status, "closed")
	}
	if snap.Verdict != "api degraded — replica count low" {
		t.Errorf("verdict: got %q", snap.Verdict)
	}
}

func TestIncidentSnapshot_FactsSurfaceAndShrinkAlive(t *testing.T) {
	h, incidentID := startPlanFixture(t)

	_, _ = h.FactAdd(FactAddParams{
		IncidentID: incidentID,
		Component:  "api",
		Key:        "operator_says_healthy",
		Value:      true,
	})

	snap, err := h.IncidentSnapshot(IncidentSnapshotParams{IncidentID: incidentID})
	if err != nil {
		t.Fatalf("IncidentSnapshot: %v", err)
	}
	if len(snap.Facts) != 1 {
		t.Fatalf("facts: got %d want 1", len(snap.Facts))
	}
	if snap.Facts[0].Component != "api" || snap.Facts[0].Key != "operator_says_healthy" {
		t.Errorf("fact identity: got (%q,%q)", snap.Facts[0].Component, snap.Facts[0].Key)
	}
	// The only scenario (api.stopped) is contradicted.
	if len(snap.SurvivingScenarios) != 0 {
		t.Errorf("surviving: got %d want 0 after contradicting fact", len(snap.SurvivingScenarios))
	}
	// Eliminated scenarios are the inverse.
	if len(snap.EliminatedScenarios) == 0 {
		t.Error("eliminated: expected at least one scenario to be eliminated")
	}
}

func TestIncidentSnapshot_IsJSONRoundTrippable(t *testing.T) {
	h, incidentID := startPlanFixture(t)
	snap, err := h.IncidentSnapshot(IncidentSnapshotParams{IncidentID: incidentID})
	if err != nil {
		t.Fatalf("IncidentSnapshot: %v", err)
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back IncidentSnapshotResult
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.IncidentID != snap.IncidentID {
		t.Error("incident_id did not round-trip")
	}
}

func TestIncidentSnapshot_MissingIncidentError(t *testing.T) {
	h := NewHandler(Config{})
	if _, err := h.IncidentSnapshot(IncidentSnapshotParams{IncidentID: "ghost"}); err == nil {
		t.Fatal("expected error for unknown incident")
	}
}
