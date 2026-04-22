package mcp

import (
	"os"
	"testing"
)

// startProbeFixture shares setup with plan tests. The generic.component
// type carries a probe with an empty cmd ("operator prompt"), which makes
// it natural to exercise the render-only + operator-prompt branches.
func startProbeFixture(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MGTT_HOME", dir)

	modelPath := writeMinimalModel(t, dir)
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	h := NewHandler(Config{})
	start, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath, ID: "inc-probe"})
	if err != nil {
		t.Fatalf("IncidentStart: %v", err)
	}
	return h, start.IncidentID
}

func TestProbe_RenderOnlyReturnsRenderedCommand(t *testing.T) {
	h, incidentID := startProbeFixture(t)

	result, err := h.Probe(ProbeParams{
		IncidentID: incidentID,
		Execute:    false,
	})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Status != "rendered" {
		t.Errorf("status: got %q want %q", result.Status, "rendered")
	}
	// Generic.component has component=api, fact=operator_says_healthy.
	if result.Component != "api" || result.Fact != "operator_says_healthy" {
		t.Errorf("identity: got (%q,%q) want (api, operator_says_healthy)", result.Component, result.Fact)
	}
}

func TestProbe_ExecuteRecognisesOperatorPromptProbe(t *testing.T) {
	// generic.component's probe has cmd: "" — the CLI treats that as an
	// operator-prompt. The MCP handler can't prompt a human; it returns a
	// specific status so the agent knows to gather the fact another way.
	h, incidentID := startProbeFixture(t)

	result, err := h.Probe(ProbeParams{
		IncidentID: incidentID,
		Execute:    true,
	})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Status != "operator_prompt_required" {
		t.Errorf("status: got %q want %q", result.Status, "operator_prompt_required")
	}
	// No fact appended for prompt probes — that's fact.add's job.
	list, _ := h.FactsList(FactsListParams{IncidentID: incidentID})
	if len(list.Facts) != 0 {
		t.Errorf("operator-prompt probe must not append a fact; got %d", len(list.Facts))
	}
}

func TestProbe_MissingIncidentError(t *testing.T) {
	h := NewHandler(Config{})
	_, err := h.Probe(ProbeParams{IncidentID: "ghost", Execute: false})
	if err == nil {
		t.Fatal("expected error for unknown incident_id")
	}
}

func TestProbe_EmptyIncidentIDError(t *testing.T) {
	h := NewHandler(Config{})
	if _, err := h.Probe(ProbeParams{Execute: false}); err == nil {
		t.Fatal("expected error when incident_id empty")
	}
}

func TestProbe_NoSuggestionStatus(t *testing.T) {
	// Fake a satisfied incident by pre-adding the healthy fact before probe.
	// With operator_says_healthy=true the engine should find no probe to run.
	h, incidentID := startProbeFixture(t)
	if _, err := h.FactAdd(FactAddParams{
		IncidentID: incidentID,
		Component:  "api",
		Key:        "operator_says_healthy",
		Value:      true,
	}); err != nil {
		t.Fatalf("FactAdd: %v", err)
	}
	result, err := h.Probe(ProbeParams{IncidentID: incidentID, Execute: false})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Status != "no_suggestion" {
		t.Errorf("status: got %q want %q", result.Status, "no_suggestion")
	}
}
