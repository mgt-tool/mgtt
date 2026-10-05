// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// datastore is minishop's type: healthy and the default state agree.
func datastore(t *testing.T) *providersupport.Type {
	t.Helper()
	parse := func(s string) expr.Node {
		n, err := expr.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	return &providersupport.Type{
		Name: "datastore",
		Facts: map[string]*providersupport.FactSpec{
			"available":        {TypeName: "mgtt.bool"},
			"connection_count": {TypeName: "mgtt.int"},
		},
		HealthyRaw: []string{"available == true", "connection_count < 500"},
		Healthy:    []expr.Node{parse("available == true"), parse("connection_count < 500")},
		States: []providersupport.StateDef{
			{Name: "live", When: parse("available == true & connection_count < 500")},
			{Name: "saturated", When: parse("available == true & connection_count >= 500")},
			{Name: "stopped", When: parse("available == false")},
		},
		DefaultActiveState: "live",
	}
}

func TestHealthStateDisagreements(t *testing.T) {
	ty := datastore(t)
	if d := model.HealthStateDisagreements(ty.Healthy, ty, "store", nil); len(d) != 0 {
		t.Fatalf("the type agrees with itself; got %+v", d)
	}
	loose, _ := expr.Parse("available == true")
	d := model.HealthStateDisagreements([]expr.Node{loose}, ty, "store", nil)
	if len(d) != 1 || d[0].State != "saturated" || !d[0].Healthy || d[0].Witness["connection_count"] != 500 {
		t.Fatalf("dropping the pool rule should make saturated healthy at the boundary; got %+v", d)
	}
}

// Failing rules that no state matches leave a failure with no state for
// the scenario engine to name: reported with State "".
func TestHealthStateDisagreements_Uncovered(t *testing.T) {
	ty := datastore(t)
	ty.States = ty.States[:2] // drop stopped: available=false matches nothing
	d := model.HealthStateDisagreements(ty.Healthy, ty, "store", nil)
	if len(d) != 1 || d[0].State != "" || d[0].Healthy || d[0].Witness["available"] != false {
		t.Fatalf("available=false fails the rules with no state; got %+v", d)
	}
}

// validate reports what the component's own override introduces, not
// what it inherits; healthy_diverges_from acknowledges a state, and must
// name a real one.
func TestValidate_HealthMatchesStates(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "providers", "shop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `meta: { name: shop, version: 1.0.0, description: t }
install: { source: { build: b.sh, clean: c.sh } }
types:
  datastore:
    facts:
      available: { type: mgtt.bool, probe: { cmd: x, parse: bool } }
      connection_count: { type: mgtt.int, probe: { cmd: x, parse: int } }
    healthy: ["available == true", "connection_count < 500"]
    states:
      live: { when: "available == true & connection_count < 500" }
      saturated: { when: "available == true & connection_count >= 500" }
      stopped: { when: "available == false" }
    default_active_state: live
`
	for name, body := range map[string]string{"manifest.yaml": manifest, "b.sh": "", "c.sh": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p, err := providersupport.LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := providersupport.NewRegistry()
	reg.Register(p)
	warn := func(comp string) []string {
		m, err := model.LoadBytes([]byte("meta: {name: m, version: \"1\", providers: [shop]}\ncomponents:\n"+comp), "")
		if err != nil {
			t.Fatal(err)
		}
		res := model.Validate(m, reg)
		var out []string
		for _, w := range res.Warnings {
			if strings.Contains(w.Message, "simulate and diagnose") {
				out = append(out, w.Message)
			}
		}
		for _, e := range res.Errors {
			out = append(out, "ERROR "+e.Message)
		}
		return out
	}
	if got := warn("  db: { type: datastore }\n"); len(got) != 0 {
		t.Errorf("no override, nothing to report: %q", got)
	}
	if got := warn("  db: { type: datastore, healthy: { replace: [available == true] } }\n"); len(got) != 1 || !strings.Contains(got[0], "state saturated") {
		t.Errorf("the loosened override makes saturated healthy: %q", got)
	}
	if got := warn("  db: { type: datastore, healthy: { replace: [available == true] }, healthy_diverges_from: [saturated] }\n"); len(got) != 0 {
		t.Errorf("an acknowledged state is not reported: %q", got)
	}
	if got := warn("  db: { type: datastore, healthy_diverges_from: [melted] }\n"); len(got) != 1 || !strings.HasPrefix(got[0], "ERROR") {
		t.Errorf("acknowledging a state the type lacks is an error: %q", got)
	}
}
