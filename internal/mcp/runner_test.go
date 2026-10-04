// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// installRunnerProvider installs a provider whose facts declare no cmd and
// are answered by its own runner binary — the shape of mgtt-provider-aws.
// The binary answers available=true, or, when mode is "forbidden", exits 3
// the way a provider reports an IAM refusal.
func installRunnerProvider(t *testing.T, mgttHome, mode string) {
	t.Helper()
	dir := filepath.Join(mgttHome, "providers", "testrunner")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, body := range map[string]string{
		"build.sh": "#!/bin/sh\n",
		"clean.sh": "#!/bin/sh\n",
		"bin/mgtt-provider-testrunner": `#!/bin/sh
if [ "$(cat "$(dirname "$0")/../mode")" = forbidden ]; then
  echo "AccessDenied: not authorized" >&2
  exit 3
fi
echo '{"value": true, "raw": "true"}'
`,
		"mode": mode,
		"manifest.yaml": `meta:
  name: testrunner
  version: 1.0.0
  description: test-only provider answered by its runner binary
install:
  source:
    build: build.sh
    clean: clean.sh
types:
  queue:
    facts:
      available:
        type: mgtt.bool
        probe:
          parse: bool
    healthy:
      - "available == true"
    states:
      live:
        when: "available == true"
      down:
        when: "available == false"
    default_active_state: live
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func startRunnerFixture(t *testing.T, mode string) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MGTT_HOME", dir)
	installRunnerProvider(t, dir, mode)
	modelPath := filepath.Join(dir, "system.model.yaml")
	model := `meta:
  name: runnershop
  version: "1.0"
  providers: [testrunner]
components:
  mq:
    type: queue
`
	if err := os.WriteFile(modelPath, []byte(model), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	t.Chdir(dir)
	h := NewHandler(Config{})
	start, err := h.IncidentStart(IncidentStartParams{ModelRef: modelPath, ID: "inc-runner"})
	if err != nil {
		t.Fatalf("IncidentStart: %v", err)
	}
	return h, start.IncidentID
}

// A fact with no cmd is not an operator prompt when the provider has a
// runner: MCP runs it through the runner, exactly as `mgtt diagnose` does.
func TestProbe_RunnerProviderRunsWithoutCmd(t *testing.T) {
	h, id := startRunnerFixture(t, "ok")

	result, err := h.Probe(ProbeParams{IncidentID: id, Execute: true})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Status != "executed" || result.Value != true {
		t.Fatalf("got status %q value %v (reason %q), want executed true", result.Status, result.Value, result.Reason)
	}
	list, _ := h.FactsList(FactsListParams{IncidentID: id})
	if len(list.Facts) != 1 || list.Facts[0].Value != true {
		t.Fatalf("want one recorded fact mq.available=true, got %+v", list.Facts)
	}
}

// A refused probe is recorded as an unknown fact, as the CLI records it,
// so the engine neither clears the component nor re-suggests the probe.
func TestProbe_ForbiddenIsRecordedAsUnknown(t *testing.T) {
	h, id := startRunnerFixture(t, "forbidden")

	result, err := h.Probe(ProbeParams{IncidentID: id, Execute: true})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Status != "forbidden" {
		t.Fatalf("status = %q (reason %q), want forbidden", result.Status, result.Reason)
	}
	list, _ := h.FactsList(FactsListParams{IncidentID: id})
	if len(list.Facts) != 1 || list.Facts[0].Status != "forbidden" || list.Facts[0].Value != nil {
		t.Fatalf("want one value-less forbidden fact, got %+v", list.Facts)
	}
	plan, err := h.Plan(PlanParams{IncidentID: id})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.CannotRuleOut) != 1 || plan.CannotRuleOut[0].Component != "mq" || plan.CannotRuleOut[0].Facts["available"] != "forbidden" {
		t.Fatalf("plan must report mq as not ruled out; got %+v", plan.CannotRuleOut)
	}
}
