package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// writeMinimalModel drops a valid system.model.yaml into dir and returns
// the path. The model has one generic component so model.Load accepts it
// without requiring a provider registry.
func writeMinimalModel(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "system.model.yaml")
	body := `meta:
  name: storefront
  version: "1.0"
  providers: [generic]
components:
  api:
    type: component
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	return path
}

func TestIncidentStart_CreatesPersistentIncidentWithoutCurrentPointer(t *testing.T) {
	dir := t.TempDir()
	modelPath := writeMinimalModel(t, dir)

	// State files land in cwd, so run the handler from the temp dir.
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(orig)

	h := NewHandler(Config{})
	result, err := h.IncidentStart(IncidentStartParams{
		ModelRef: modelPath,
		ID:       "test-mcp-001",
	})
	if err != nil {
		t.Fatalf("IncidentStart: %v", err)
	}
	if result.IncidentID != "test-mcp-001" {
		t.Errorf("incident_id: got %q want %q", result.IncidentID, "test-mcp-001")
	}

	// State file exists at the canonical name the CLI also expects.
	stateFile := result.IncidentID + ".state.yaml"
	if _, err := os.Stat(stateFile); err != nil {
		t.Errorf("state file not created: %v", err)
	}

	// Design D5: concurrent incidents. The MCP path must NOT write the
	// CLI's single-active pointer, or a human running `mgtt incident start`
	// later would be refused.
	if _, err := os.Stat(".mgtt-current"); !os.IsNotExist(err) {
		t.Error(".mgtt-current must not be written by the MCP handler")
	}
}

func TestIncidentStart_GeneratesIDWhenOmitted(t *testing.T) {
	dir := t.TempDir()
	modelPath := writeMinimalModel(t, dir)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	h := NewHandler(Config{})
	result, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath})
	if err != nil {
		t.Fatalf("IncidentStart: %v", err)
	}
	if result.IncidentID == "" {
		t.Fatal("expected a generated incident_id when ID omitted")
	}
}

func TestIncidentStart_MissingModelReturnsError(t *testing.T) {
	h := NewHandler(Config{})
	_, err := h.IncidentStart(IncidentStartParams{
		ModelRef: "/definitely/not/a/real/path.yaml",
	})
	if err == nil {
		t.Fatal("expected error for missing model_ref")
	}
}

func TestIncidentEnd_ClosesIncidentAndReportsSaved(t *testing.T) {
	dir := t.TempDir()
	modelPath := writeMinimalModel(t, dir)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	h := NewHandler(Config{})
	start, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath, ID: "inc-e2e"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	end, err := h.IncidentEnd(IncidentEndParams{
		IncidentID: start.IncidentID,
		Verdict:    "api was crash-looping",
	})
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	if !end.Saved {
		t.Error("saved: got false, want true")
	}
}

func TestIncidentEnd_MissingIncidentReturnsError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	h := NewHandler(Config{})
	if _, err := h.IncidentEnd(IncidentEndParams{IncidentID: "ghost"}); err == nil {
		t.Fatal("expected error for missing incident id")
	}
}

func TestIncidentEnd_MissingIDIsError(t *testing.T) {
	h := NewHandler(Config{})
	if _, err := h.IncidentEnd(IncidentEndParams{}); err == nil {
		t.Fatal("expected error when incident_id empty")
	}
}

func TestIncidentStart_AllowsParallelIncidentsInSameDir(t *testing.T) {
	// Two agents driving two incidents against the same $MGTT_HOME is the
	// whole point of D5. The CLI's single-active check (`incident already
	// in progress`) must not apply here.
	dir := t.TempDir()
	modelPath := writeMinimalModel(t, dir)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	h := NewHandler(Config{})
	if _, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath, ID: "a"}); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if _, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath, ID: "b"}); err != nil {
		t.Fatalf("second start must not be blocked by CLI-style single-active invariant: %v", err)
	}
}
