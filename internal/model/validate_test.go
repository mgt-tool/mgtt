// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model_test

import (
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// TestValidate_DuplicateResourceWarning — two components of the same
// type pointing at the same resource are almost always a copy-paste
// mistake. Emit a warning (not an error — there are legitimate cases
// for two probes against one resource, e.g. different fact sets).
func TestValidate_DuplicateResourceWarning(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "dup", Version: "1.0", Providers: []string{"aws"}},
		Components: map[string]*model.Component{
			"rds_primary": {Name: "rds_primary", Type: "rds_instance", Resource: "flowers-stage"},
			"rds_alias":   {Name: "rds_alias", Type: "rds_instance", Resource: "flowers-stage"},
		},
		Order: []string{"rds_primary", "rds_alias"},
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "aws"},
		Types: map[string]*providersupport.Type{"rds_instance": {Name: "rds_instance"}},
	})

	result := model.Validate(m, reg)

	if len(result.Warnings) == 0 {
		t.Fatalf("expected at least one warning; got 0")
	}
	var found bool
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "flowers-stage") && strings.Contains(w.Message, "duplicate") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected duplicate-resource warning naming 'flowers-stage'; got %+v", result.Warnings)
	}
}

// A Service and the Deployment behind it share a name; keyed by kind they
// stand apart, but a kind-prefixed key and a plain one of the same type
// read the same object, and a prefix that is not the type reads whole.
func TestValidate_KindPrefixedKeys(t *testing.T) {
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "kubernetes"},
		Types: map[string]*providersupport.Type{
			"service": {Name: "service"}, "deployment": {Name: "deployment"},
		},
	})
	warnings := func(comps ...*model.Component) []string {
		m := &model.Model{
			Meta:       model.Meta{Name: "k", Version: "1.0", Providers: []string{"kubernetes"}},
			Components: map[string]*model.Component{},
		}
		for _, c := range comps {
			m.Components[c.Name] = c
			m.Order = append(m.Order, c.Name)
		}
		var out []string
		for _, w := range model.Validate(m, reg).Warnings {
			out = append(out, w.Message)
		}
		return out
	}
	if w := warnings(
		&model.Component{Name: "service/acme", Type: "service"},
		&model.Component{Name: "acme", Type: "deployment"},
	); len(w) != 0 {
		t.Errorf("a Service and a Deployment named acme stand apart; got %v", w)
	}
	if w := warnings(
		&model.Component{Name: "service/acme", Type: "service"},
		&model.Component{Name: "acme", Type: "service"},
	); len(w) != 1 || !strings.Contains(w[0], `duplicate resource "acme"`) {
		t.Errorf("service/acme and acme both read Service acme; got %v", w)
	}
	if w := warnings(&model.Component{Name: "svc/acme", Type: "service"}); len(w) != 1 || !strings.Contains(w[0], "service/acme") {
		t.Errorf("svc is not the type: want a warning naming service/acme; got %v", w)
	}
	if w := warnings(&model.Component{Name: "svc/acme", Type: "service", Resource: "acme"}); len(w) != 0 {
		t.Errorf("resource: says what to probe; got %v", w)
	}
}

// A component's healthy: list replaces its type's rules, it does not add
// to them. Dropping a type rule that way is the most common modelling
// mistake (an RDS override that loses `available == true` calls a stopped
// database healthy), so every dropped rule is named in a warning.
func TestValidate_HealthyOverrideDropsTypeRule(t *testing.T) {
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "aws"},
		Types: map[string]*providersupport.Type{"rds_instance": {
			Name:       "rds_instance",
			HealthyRaw: []string{"available == true", "connection_count < 500"},
		}},
	})
	m := &model.Model{
		Meta: model.Meta{Name: "ov", Version: "1.0", Providers: []string{"aws"}},
		Components: map[string]*model.Component{
			"loose":    {Name: "loose", Type: "rds_instance", HealthyRaw: []string{"connection_count < 900"}},
			"restated": {Name: "restated", Type: "rds_instance", HealthyRaw: []string{"available==true", "connection_count<500", "replica_lag < 30"}},
			"default":  {Name: "default", Type: "rds_instance"},
		},
		Order: []string{"loose", "restated", "default"},
	}

	result := model.Validate(m, reg)

	var got []string
	for _, w := range result.Warnings {
		if w.Field == "healthy" {
			got = append(got, w.Component+": "+w.Message)
		}
	}
	// restated keeps both type rules (whitespace aside) and adds one: no
	// warning. default overrides nothing.
	if len(got) != 2 {
		t.Fatalf("want two warnings, both on loose; got %q", got)
	}
	for _, rule := range []string{"available == true", "connection_count < 500"} {
		if !strings.Contains(strings.Join(got, "\n"), "loose: healthy override drops type rule \""+rule+"\"") {
			t.Errorf("no warning naming dropped rule %q; got %q", rule, got)
		}
	}
}

// A rule comparing against a var nothing sets can never be decided, so the
// component can never be shown healthy; validate says so, once per var.
func TestValidate_UnsetVarWarning(t *testing.T) {
	when, err := expr.Parse("restart_count <= max_restart_count")
	if err != nil {
		t.Fatal(err)
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "docker"},
		Types: map[string]*providersupport.Type{"container": {
			Name:    "container",
			Facts:   map[string]*providersupport.FactSpec{"restart_count": {TypeName: "mgtt.int"}},
			Healthy: []expr.Node{when},
			States:  []providersupport.StateDef{{Name: "live", When: when}},
		}},
	})
	m := &model.Model{
		Meta: model.Meta{Name: "v", Providers: []string{"docker"}},
		Components: map[string]*model.Component{
			"set":   {Name: "set", Type: "container", Vars: map[string]string{"max_restart_count": "5"}},
			"unset": {Name: "unset", Type: "container"},
		},
		Order: []string{"set", "unset"},
	}
	var got []string
	for _, w := range model.Validate(m, reg).Warnings {
		if w.Field == "vars" {
			got = append(got, w.Component)
		}
	}
	if len(got) != 1 || got[0] != "unset" {
		t.Fatalf("want one vars warning, on unset; got %v", got)
	}
}

func TestValidate_Source(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "s", Version: "1"},
		Components: map[string]*model.Component{
			"a": {Name: "a", Type: "x", Source: "discovered"},
			"b": {Name: "b", Type: "x", Source: "authored"},
			"c": {Name: "c", Type: "x"},
			"d": {Name: "d", Type: "x", Source: "found"},
		},
		Order: []string{"a", "b", "c", "d"},
	}
	var bad []string
	for _, e := range model.Validate(m, nil).Errors {
		if e.Field == "source" {
			bad = append(bad, e.Component)
		}
	}
	if len(bad) != 1 || bad[0] != "d" {
		t.Fatalf("only d's source is invalid; got %v", bad)
	}
}
