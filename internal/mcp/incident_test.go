// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"os"
	"path/filepath"
	"strings"
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
	t.Chdir(dir)

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
	t.Chdir(dir)

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
	t.Chdir(dir)

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
	t.Chdir(dir)

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
	t.Chdir(dir)

	h := NewHandler(Config{})
	if _, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath, ID: "a"}); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if _, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath, ID: "b"}); err != nil {
		t.Fatalf("second start must not be blocked by CLI-style single-active invariant: %v", err)
	}
}

// emit_scenario records an MCP incident -- found by its model_ref, not
// .mgtt-current -- as a scenario next to the model, and reports it.
func TestIncidentEnd_EmitScenario(t *testing.T) {
	cases := []struct {
		name        string
		facts       map[string]any // api fact → value
		existing    bool
		wantWarning string
		wantYAML    []string
	}{
		{
			name:     "operator says api is broken",
			facts:    map[string]any{"operator_says_healthy": false},
			wantYAML: []string{"name: inc-emit\n", "inject:\n  api:\n    operator_says_healthy: false\n"},
		},
		{
			name:        "no facts",
			wantWarning: "incident recorded no facts — nothing to emit",
		},
		{
			name:        "existing file is kept",
			facts:       map[string]any{"operator_says_healthy": false},
			existing:    true,
			wantWarning: "already exists — not overwritten",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("MGTT_HOME", dir)
			modelDir := filepath.Join(dir, "model")
			if err := os.MkdirAll(modelDir, 0o755); err != nil {
				t.Fatal(err)
			}
			modelPath := writeMinimalModel(t, modelDir)
			t.Chdir(dir)
			scenarioPath := filepath.Join(modelDir, "scenarios", "inc-emit.yaml")
			if tc.existing {
				if err := os.MkdirAll(filepath.Dir(scenarioPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(scenarioPath, []byte("kept\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			h := NewHandler(Config{})
			if _, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath, ID: "inc-emit"}); err != nil {
				t.Fatalf("start: %v", err)
			}
			for k, v := range tc.facts {
				if _, err := h.FactAdd(FactAddParams{IncidentID: "inc-emit", Component: "api", Key: k, Value: v}); err != nil {
					t.Fatalf("fact_add: %v", err)
				}
			}
			// The recorded expectation is what plan concludes from these facts.
			plan, err := h.Plan(PlanParams{IncidentID: "inc-emit"})
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			wantRoot := plan.RootCause
			if wantRoot == "" {
				wantRoot = "none"
			}
			end, err := h.IncidentEnd(IncidentEndParams{IncidentID: "inc-emit", EmitScenario: true})
			if err != nil {
				t.Fatalf("end: %v", err)
			}
			if !end.Saved {
				t.Error("saved: got false, want true")
			}
			if tc.wantWarning != "" {
				if !strings.Contains(end.ScenarioWarning, tc.wantWarning) {
					t.Errorf("scenario_warning = %q, want it to contain %q", end.ScenarioWarning, tc.wantWarning)
				}
				if end.ScenarioYAML != "" || end.ScenarioPasses != nil {
					t.Errorf("nothing was written, yet result = %+v", end)
				}
				if tc.existing {
					if end.ScenarioPath != scenarioPath {
						t.Errorf("scenario_path = %q, want the existing %q", end.ScenarioPath, scenarioPath)
					}
					if data, _ := os.ReadFile(scenarioPath); string(data) != "kept\n" {
						t.Errorf("existing scenario overwritten: %q", data)
					}
				}
				return
			}
			if end.ScenarioPath != scenarioPath {
				t.Errorf("scenario_path = %q, want %q", end.ScenarioPath, scenarioPath)
			}
			if end.ScenarioPasses == nil || !*end.ScenarioPasses {
				t.Errorf("scenario_passes = %v, want true", end.ScenarioPasses)
			}
			onDisk, err := os.ReadFile(scenarioPath)
			if err != nil || string(onDisk) != end.ScenarioYAML {
				t.Errorf("scenario_yaml differs from the file (err %v)", err)
			}
			for _, want := range append(tc.wantYAML, "expect:\n  root_cause: "+wantRoot+"\n") {
				if !strings.Contains(end.ScenarioYAML, want) {
					t.Errorf("scenario_yaml missing %q:\n%s", want, end.ScenarioYAML)
				}
			}
		})
	}
}

// Without emit_scenario the result carries no scenario fields at all.
func TestIncidentEnd_NoEmitScenarioLeavesFieldsEmpty(t *testing.T) {
	dir := t.TempDir()
	modelPath := writeMinimalModel(t, dir)
	t.Chdir(dir)
	h := NewHandler(Config{})
	if _, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath, ID: "inc-plain"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := h.FactAdd(FactAddParams{IncidentID: "inc-plain", Component: "api", Key: "operator_says_healthy", Value: false}); err != nil {
		t.Fatalf("fact_add: %v", err)
	}
	end, err := h.IncidentEnd(IncidentEndParams{IncidentID: "inc-plain"})
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	if *end != (IncidentEndResult{Saved: true}) {
		t.Errorf("result = %+v, want only saved", end)
	}
	if _, err := os.Stat(filepath.Join(dir, "scenarios")); !os.IsNotExist(err) {
		t.Error("scenarios/ written without emit_scenario")
	}
}
