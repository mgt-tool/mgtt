package mcp

import (
	"os"
	"testing"
)

// startFixtureIncident creates a temp-dir + minimal model + a fresh
// incident, returning the handler ready for fact operations.
func startFixtureIncident(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	modelPath := writeMinimalModel(t, dir)
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	h := NewHandler(Config{})
	start, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath, ID: "inc-facts"})
	if err != nil {
		t.Fatalf("IncidentStart: %v", err)
	}
	return h, start.IncidentID
}

func TestFactAdd_AppendsFactToIncident(t *testing.T) {
	h, incidentID := startFixtureIncident(t)

	result, err := h.FactAdd(FactAddParams{
		IncidentID: incidentID,
		Component:  "api",
		Key:        "ready_replicas",
		Value:      float64(3), // JSON numbers decode as float64 in Go
	})
	if err != nil {
		t.Fatalf("FactAdd: %v", err)
	}
	if !result.Appended {
		t.Error("appended: got false, want true")
	}

	// Round-trip: facts.list sees it.
	list, err := h.FactsList(FactsListParams{IncidentID: incidentID})
	if err != nil {
		t.Fatalf("FactsList: %v", err)
	}
	if len(list.Facts) != 1 {
		t.Fatalf("facts count: got %d want 1", len(list.Facts))
	}
	got := list.Facts[0]
	if got.Component != "api" || got.Key != "ready_replicas" {
		t.Errorf("identity: got (%q,%q) want (api, ready_replicas)", got.Component, got.Key)
	}
}

func TestFactAdd_RecordsNote(t *testing.T) {
	h, incidentID := startFixtureIncident(t)
	_, err := h.FactAdd(FactAddParams{
		IncidentID: incidentID,
		Component:  "api",
		Key:        "health_status",
		Value:      "degraded",
		Note:       "operator override, RBAC refused the probe",
	})
	if err != nil {
		t.Fatalf("FactAdd: %v", err)
	}
	list, _ := h.FactsList(FactsListParams{IncidentID: incidentID})
	if list.Facts[0].Note != "operator override, RBAC refused the probe" {
		t.Errorf("note not preserved: %q", list.Facts[0].Note)
	}
}

func TestFactAdd_MissingRequiredFieldsError(t *testing.T) {
	h, incidentID := startFixtureIncident(t)
	cases := []FactAddParams{
		{IncidentID: "", Component: "api", Key: "k", Value: 1},       // no incident
		{IncidentID: incidentID, Component: "", Key: "k", Value: 1},  // no component
		{IncidentID: incidentID, Component: "api", Key: "", Value: 1}, // no key
	}
	for _, p := range cases {
		if _, err := h.FactAdd(p); err == nil {
			t.Errorf("expected error for missing field in %+v", p)
		}
	}
}

func TestFactAdd_MissingIncidentError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	h := NewHandler(Config{})
	_, err := h.FactAdd(FactAddParams{
		IncidentID: "ghost-incident",
		Component:  "api",
		Key:        "k",
		Value:      1,
	})
	if err == nil {
		t.Fatal("expected error for unknown incident_id")
	}
}

func TestFactsList_FiltersByComponent(t *testing.T) {
	h, incidentID := startFixtureIncident(t)
	h.FactAdd(FactAddParams{IncidentID: incidentID, Component: "api", Key: "k1", Value: 1})
	h.FactAdd(FactAddParams{IncidentID: incidentID, Component: "rds", Key: "k2", Value: 2})
	h.FactAdd(FactAddParams{IncidentID: incidentID, Component: "api", Key: "k3", Value: 3})

	list, err := h.FactsList(FactsListParams{IncidentID: incidentID, Component: "api"})
	if err != nil {
		t.Fatalf("FactsList: %v", err)
	}
	if len(list.Facts) != 2 {
		t.Fatalf("filtered count: got %d want 2", len(list.Facts))
	}
	for _, f := range list.Facts {
		if f.Component != "api" {
			t.Errorf("filter leaked component %q", f.Component)
		}
	}
}

func TestFactsList_EmptyWhenNoFactsYet(t *testing.T) {
	h, incidentID := startFixtureIncident(t)
	list, err := h.FactsList(FactsListParams{IncidentID: incidentID})
	if err != nil {
		t.Fatalf("FactsList: %v", err)
	}
	if len(list.Facts) != 0 {
		t.Errorf("expected empty list, got %d", len(list.Facts))
	}
}

func TestFactsList_MissingIncidentError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	h := NewHandler(Config{})
	if _, err := h.FactsList(FactsListParams{IncidentID: "ghost"}); err == nil {
		t.Fatal("expected error for unknown incident")
	}
}
