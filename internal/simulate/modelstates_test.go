// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"slices"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// The provider owns what facts mean; the model owns what they mean for a
// node. deploy drains to zero as a healthy state of its own (the type's
// healthy_in); worker's model calls a drained worker broken, and flaky's
// model tolerates degraded.
func modelStatesFixture(t *testing.T, body string) (*model.Model, *providersupport.Registry) {
	t.Helper()
	parse := func(s string) expr.Node {
		n, err := expr.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	deploy := &providersupport.Type{
		Name:    "deploy",
		Facts:   map[string]*providersupport.FactSpec{"desired": {TypeName: "mgtt.int"}, "ready": {TypeName: "mgtt.int"}},
		Healthy: []expr.Node{parse("ready >= desired")},
		States: []providersupport.StateDef{
			{Name: "drained", When: parse("desired == 0"), Verdict: providersupport.VerdictHealthy},
			{Name: "live", When: parse("ready >= desired")},
			{Name: "degraded", When: parse("ready < desired")},
		},
		DefaultActiveState: "live",
		FailureModes:       map[string][]string{"degraded": {"upstream_failure"}},
	}
	svc := &providersupport.Type{
		Name:               "svc",
		Facts:              map[string]*providersupport.FactSpec{"up": {TypeName: "mgtt.bool"}},
		Healthy:            []expr.Node{parse("up == true")},
		States:             []providersupport.StateDef{{Name: "live", When: parse("up == true")}, {Name: "down", When: parse("up == false")}},
		DefaultActiveState: "live",
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{Meta: providersupport.ProviderMeta{Name: "p"}, Types: map[string]*providersupport.Type{"deploy": deploy, "svc": svc}})
	m, err := model.LoadBytes([]byte("meta:\n  name: m\n  version: \"1.0\"\n  providers: [p]\n  scenarios: none\ncomponents:\n"+body), "system.model.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return m, reg
}

const modelStatesBody = `  front:
    type: svc
    depends: [{on: worker}, {on: idle}, {on: flaky}]
  worker:
    type: deploy
    states:
      drained_here:
        when: desired == 0
        can_cause: [upstream_failure]
  idle:
    type: deploy
  flaky:
    type: deploy
    healthy_in: [degraded]
`

func TestModelStates_TheModelHasTheLastWord(t *testing.T) {
	m, reg := modelStatesFixture(t, modelStatesBody)
	if errs := model.Validate(m, reg).Errors; len(errs) != 0 {
		t.Fatalf("valid model: %+v", errs)
	}
	run := func(inject map[string]map[string]any) Expectation {
		return Run(m, reg, &Scenario{Inject: inject}).Actual
	}
	healthy := map[string]any{"desired": 2, "ready": 2}
	drained := map[string]any{"desired": 0, "ready": 0}

	// idle drained: the type calls draining healthy, so front's own failure is the root.
	got := run(map[string]map[string]any{"front": {"up": false}, "worker": healthy, "idle": drained, "flaky": healthy})
	if got.RootCause != "front" {
		t.Errorf("a drained idle deployment is healthy by its type; root cause %q, want front", got.RootCause)
	}
	// worker drained: the model calls it broken, whatever the type says.
	got = run(map[string]map[string]any{"front": {"up": false}, "worker": drained, "idle": healthy, "flaky": healthy})
	if got.RootCause != "worker" {
		t.Errorf("the model calls a drained worker broken; root cause %q, want worker", got.RootCause)
	}
	// flaky degraded: the model tolerates it, though the rules fail.
	got = run(map[string]map[string]any{"front": {"up": false}, "worker": healthy, "idle": healthy, "flaky": {"desired": 3, "ready": 1}})
	if got.RootCause != "front" {
		t.Errorf("flaky is healthy in degraded by its model; root cause %q, want front", got.RootCause)
	}

	// Enumeration agrees: worker's own state starts chains, a healthy state starts none.
	roots := map[string]bool{}
	for _, s := range scenarios.Enumerate(m, reg) {
		roots[s.Root.Component+"."+s.Root.State] = true
	}
	if !roots["worker.drained_here"] || roots["idle.drained"] || roots["flaky.degraded"] {
		t.Errorf("chain roots %v: want worker.drained_here, and no chain from a healthy state", roots)
	}

	// And so does the consistency check: rules and verdicts agree everywhere.
	for _, name := range []string{"worker", "idle", "flaky"} {
		c := m.Components[name]
		ty, _, _ := c.ResolveType(m, reg)
		if d := model.HealthStateDisagreements(c.HealthyRules(ty), ty, name, nil); len(d) != 0 {
			t.Errorf("%s: %+v", name, d)
		}
	}
}

func TestModelStates_Validation(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{"  w:\n    type: deploy\n    states:\n      live:\n        when: desired == 0\n", "already a state"},
		{"  w:\n    type: deploy\n    healthy_in: [sleeping]\n", `"sleeping" is not a state`},
		{"  w:\n    type: deploy\n    states:\n      cold:\n        when: temperature < 5\n", `reads fact "temperature"`},
	} {
		m, reg := modelStatesFixture(t, c.body)
		var msgs []string
		for _, e := range model.Validate(m, reg).Errors {
			msgs = append(msgs, e.Message)
		}
		if !slices.ContainsFunc(msgs, func(s string) bool { return strings.Contains(s, c.want) }) {
			t.Errorf("want an error containing %q; got %v", c.want, msgs)
		}
	}
}
