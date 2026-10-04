// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/scenarios"
	"github.com/mgt-tool/mgtt/internal/simulate"
)

// diagnoseTwoComponentIncident runs `mgtt diagnose` against diagnoseFixture
// inside an active incident. Every probe answers "down"; api.status alone
// settles it.
func diagnoseTwoComponentIncident(t *testing.T, modelName, id string) {
	t.Helper()
	if _, err := incident.Start(modelName, "0.1.0", id); err != nil {
		t.Fatalf("incident.Start: %v", err)
	}
	m, reg := diagnoseFixture(t)
	withLoader(t, m, reg, []scenarios.Scenario{{
		ID:   "db-down",
		Root: scenarios.RootRef{Component: "db", State: "down"},
		Chain: []scenarios.Step{
			{Component: "db", State: "down", EmitsOnEdge: "timeout"},
			{Component: "api", State: "down", Observes: []string{"status"}},
		},
	}, {
		ID:    "api-down",
		Root:  scenarios.RootRef{Component: "api", State: "down"},
		Chain: []scenarios.Step{{Component: "api", State: "down", Observes: []string{"status"}}},
	}})
	withRunner(t, &stubProbeRunner{values: map[string]any{"api.status": "down", "db.status": "down"}})
	out, err := runDiagnoseCaptured(t, diagnoseFlags{maxProbes: 10, deadline: 5 * time.Second, onWrite: "pause", readonlyOnly: true})
	if err != nil {
		t.Fatalf("runDiagnose: %v\nout: %s", err, out)
	}
}

// diagnose used to keep probed facts in memory only, so the incident's
// state file ended with `facts: {}` and nothing downstream could read them.
func TestDiagnose_PersistsProbedFactsToIncident(t *testing.T) {
	chdirTempDir(t)
	diagnoseTwoComponentIncident(t, "persist", "inc-persist-001")

	reloaded, err := incident.LoadByID("inc-persist-001")
	if err != nil {
		t.Fatalf("LoadByID: %v", err)
	}
	if f := reloaded.Store.Latest("api", "status"); f == nil || f.Value != "down" {
		t.Errorf("state file: api.status = %+v, want down", f)
	}
}

// The real flow: incident start → diagnose → incident end --emit-scenario.
// The written file must replay green through `mgtt simulate`.
func TestIncidentEnd_EmitScenario_AfterDiagnose(t *testing.T) {
	dir := chdirTempDir(t)
	modelPath := writeTwoComponentModel(t, dir, "emit-flow")
	stubLoadModelAndRegistry(t, modelPath)
	diagnoseTwoComponentIncident(t, "emit-flow", "inc-emit-001")

	var stdout, stderr bytes.Buffer
	cmd := newIncidentEndCmd()
	cmd.SetArgs([]string{"--emit-scenario"})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("incident end: %v", err)
	}
	want := "wrote " + filepath.Join(dir, "scenarios", "inc-emit-001.yaml") + " — passes; commit it to keep this diagnosis under test\n"
	if !strings.HasSuffix(stdout.String(), want) {
		t.Errorf("stdout:\n%s\nwant suffix:\n%s", stdout.String(), want)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %s", stderr.String())
	}

	sc, err := simulate.LoadScenario(filepath.Join("scenarios", "inc-emit-001.yaml"))
	if err != nil {
		t.Fatalf("load emitted scenario: %v", err)
	}
	if sc.Name != "inc-emit-001" || sc.Inject["api"]["status"] != "down" {
		t.Errorf("emitted scenario = %+v", sc)
	}
	m, reg := diagnoseFixture(t)
	if r := simulate.Run(m, reg, sc); !r.Pass {
		t.Errorf("emitted scenario fails simulate: expected %+v, actual %+v", sc.Expect, r.Actual)
	}
}

func TestIncidentEnd_EmitScenario_Reports(t *testing.T) {
	cases := []struct {
		name       string
		facts      map[string]any
		existing   bool
		wantStdout string
		wantStderr string
	}{
		{
			name:       "no facts",
			wantStdout: "incident recorded no facts — nothing to emit\n",
		},
		{
			name:       "refuses to overwrite",
			facts:      map[string]any{"db": "down"},
			existing:   true,
			wantStderr: "warning: --emit-scenario: scenarios/inc-report-001.yaml already exists — not overwritten\n",
		},
		{
			name:       "together with --suggest-scenarios",
			facts:      map[string]any{"db": "down", "api": "down"},
			wantStdout: "wrote scenarios/inc-report-001.yaml — passes; commit it to keep this diagnosis under test\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chdirTempDir(t)
			// A relative model path, as `mgtt` finds it walking cwd.
			writeTwoComponentModel(t, ".", "report")
			stubLoadModelAndRegistry(t, "model.yaml")
			seedIncidentStore(t, "report", "inc-report-001", tc.facts)
			scenarioPath := filepath.Join("scenarios", "inc-report-001.yaml")
			if tc.existing {
				if err := os.MkdirAll("scenarios", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(scenarioPath, []byte("kept\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			var stdout, stderr bytes.Buffer
			cmd := newIncidentEndCmd()
			cmd.SetArgs([]string{"--emit-scenario", "--suggest-scenarios"})
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("incident end must exit 0, got %v", err)
			}
			if tc.wantStdout != "" && !strings.HasSuffix(stdout.String(), tc.wantStdout) {
				t.Errorf("stdout:\n%s\nwant suffix:\n%s", stdout.String(), tc.wantStdout)
			}
			if tc.wantStderr != "" && stderr.String() != tc.wantStderr {
				t.Errorf("stderr = %q, want %q", stderr.String(), tc.wantStderr)
			}
			if tc.existing {
				if data, _ := os.ReadFile(scenarioPath); string(data) != "kept\n" {
					t.Errorf("existing scenario overwritten: %q", data)
				}
			}
		})
	}
}
