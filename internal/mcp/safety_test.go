package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
)

// installWriteProvider drops a minimal write-capable provider under
// $MGTT_HOME/providers/testwriter so LoadAllForUse picks it up. The
// provider declares `read_only: false` plus a single type with a real
// (safe) cmd — enough to exercise the on_write / max_execute gates.
func installWriteProvider(t *testing.T, mgttHome string) {
	t.Helper()
	dir := filepath.Join(mgttHome, "providers", "testwriter")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Drop a placeholder install/clean script — LoadFromDir checks the
	// manifest shape, not the scripts' behaviour.
	if err := os.WriteFile(filepath.Join(dir, "build.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write build.sh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clean.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write clean.sh: %v", err)
	}
	manifest := `meta:
  name: testwriter
  version: 1.0.0
  description: test-only write provider (for safety-gate tests)
read_only: false
writes_note: test-only writes; exercises on_write and max_execute gates
install:
  source:
    build: build.sh
    clean: clean.sh
types:
  service:
    facts:
      status:
        type: mgtt.string
        probe:
          cmd: "echo healthy"
          parse: string
    healthy:
      - "status == healthy"
    states:
      live:
        when: "status == healthy"
      broken:
        when: "status != healthy"
    default_active_state: live
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// writeWriteCapableModel drops a model that uses testwriter.service, so
// engine.Plan suggests a probe with a real (non-empty) cmd that reaches
// the safety gates.
func writeWriteCapableModel(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "system.model.yaml")
	body := `meta:
  name: storefront
  version: "1.0"
  providers: [testwriter]
components:
  api:
    type: service
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	return path
}

// startSafetyFixture installs the write provider, drops a model, creates
// an incident, and returns a handler configured via overrideCfg — so each
// test can tune OnWrite / ReadonlyOnly / MaxExecutePerIncident.
func startSafetyFixture(t *testing.T, cfg Config) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MGTT_HOME", dir)
	installWriteProvider(t, dir)
	modelPath := writeWriteCapableModel(t, dir)

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	h := NewHandler(cfg)
	start, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath, ID: "inc-safety"})
	if err != nil {
		t.Fatalf("IncidentStart: %v", err)
	}
	return h, start.IncidentID
}

func TestProbe_OnWriteFailBlocksWriteProvider(t *testing.T) {
	h, incidentID := startSafetyFixture(t, Config{OnWrite: "fail"})

	result, err := h.Probe(ProbeParams{IncidentID: incidentID, Execute: true})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Status != "blocked_write_fail" {
		t.Errorf("status: got %q want %q", result.Status, "blocked_write_fail")
	}
	// Rendered command is still surfaced so an agent/operator can act on it.
	if result.RenderedCommand == "" {
		t.Error("blocked probes must still surface rendered_command")
	}
}

func TestProbe_OnWritePauseBlocksWriteProvider(t *testing.T) {
	h, incidentID := startSafetyFixture(t, Config{OnWrite: "pause"})
	result, err := h.Probe(ProbeParams{IncidentID: incidentID, Execute: true})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Status != "blocked_write_pause" {
		t.Errorf("status: got %q want %q", result.Status, "blocked_write_pause")
	}
}

func TestProbe_OnWriteRunIsTheDefault(t *testing.T) {
	// OnWrite="" should behave as "run" — probe executes even on write
	// providers. The cmd `echo healthy` is safe to run in a test env.
	h, incidentID := startSafetyFixture(t, Config{})
	result, err := h.Probe(ProbeParams{IncidentID: incidentID, Execute: true})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Status != "executed" {
		t.Errorf("status: got %q want %q (default OnWrite should run)", result.Status, "executed")
	}
}

func TestProbe_ReadonlyOnlyTakesPrecedenceOverOnWrite(t *testing.T) {
	// When both readonly_only and on_write=fail are set, readonly_only
	// wins. It's the stricter / earlier gate.
	h, incidentID := startSafetyFixture(t, Config{
		ReadonlyOnly: true,
		OnWrite:      "fail",
	})
	result, err := h.Probe(ProbeParams{IncidentID: incidentID, Execute: true})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Status != "blocked_readonly" {
		t.Errorf("status: got %q want %q", result.Status, "blocked_readonly")
	}
}

func TestProbe_MaxExecuteBlocksWhenBudgetReached(t *testing.T) {
	// With a budget of 1, pre-seeding one probe-collected fact exhausts
	// the budget — next probe must be blocked_budget.
	h, incidentID := startSafetyFixture(t, Config{MaxExecutePerIncident: 1})

	inc, err := incident.LoadByID(incidentID)
	if err != nil {
		t.Fatalf("load incident: %v", err)
	}
	inc.Store.Append("api", facts.Fact{
		Key:       "prior",
		Value:     "healthy",
		Collector: "probe",
	})
	if err := inc.Store.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := h.Probe(ProbeParams{IncidentID: incidentID, Execute: true})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Status != "blocked_budget" {
		t.Errorf("status: got %q want %q", result.Status, "blocked_budget")
	}
	if result.RenderedCommand == "" {
		t.Error("blocked probes must still surface rendered_command")
	}
}

func TestProbe_MaxExecuteZeroIsUnlimited(t *testing.T) {
	// The Go zero-value for MaxExecutePerIncident is 0 — treat that as
	// "no limit" rather than "block everything".
	h, incidentID := startSafetyFixture(t, Config{MaxExecutePerIncident: 0})
	result, err := h.Probe(ProbeParams{IncidentID: incidentID, Execute: true})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Status == "blocked_budget" {
		t.Error("MaxExecutePerIncident=0 must be unlimited, not blocking")
	}
}

func TestProbe_MaxExecuteDoesNotCountAgentFacts(t *testing.T) {
	// fact.add entries (collector=agent) don't consume the probe budget.
	// A budget of 1 with 3 agent-collected facts should still allow a probe.
	h, incidentID := startSafetyFixture(t, Config{MaxExecutePerIncident: 1})
	for _, k := range []string{"a", "b", "c"} {
		if _, err := h.FactAdd(FactAddParams{
			IncidentID: incidentID,
			Component:  "api",
			Key:        k,
			Value:      "x",
		}); err != nil {
			t.Fatalf("FactAdd: %v", err)
		}
	}
	result, err := h.Probe(ProbeParams{IncidentID: incidentID, Execute: true})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Status == "blocked_budget" {
		t.Error("agent-collected facts must not consume probe budget")
	}
}
