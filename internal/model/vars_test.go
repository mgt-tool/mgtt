// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model_test

import (
	"testing"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// A var resolves from the component, then meta.vars, then the default its
// provider declares; the first that sets it wins.
func TestVarLookupPrecedence(t *testing.T) {
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "docker"},
		Types: map[string]*providersupport.Type{"container": {Name: "container"}},
		Variables: map[string]providersupport.Variable{
			"max_restart_count":    {Default: "5"},
			"acceptable_exit_code": {Default: "0"},
			"required_no_default":  {Required: true},
		},
	})
	m := &model.Model{
		Meta: model.Meta{Name: "v", Providers: []string{"docker"}, Vars: map[string]string{"max_restart_count": "3"}},
		Components: map[string]*model.Component{
			"web": {Name: "web", Type: "container", Vars: map[string]string{"max_restart_count": "1"}},
			"db":  {Name: "db", Type: "container"},
		},
		Order: []string{"web", "db"},
	}
	vars := m.VarLookup(reg)
	for _, tc := range []struct {
		comp, key, want string
		set             bool
	}{
		{"web", "max_restart_count", "1", true},   // component
		{"db", "max_restart_count", "3", true},    // meta.vars
		{"db", "acceptable_exit_code", "0", true}, // provider default
		{"db", "required_no_default", "", false},  // declared, never set
		{"db", "nobody_declares_this", "", false},
	} {
		got, set := vars.LookupVar(tc.comp, tc.key)
		if got != tc.want || set != tc.set {
			t.Errorf("%s.%s = (%q, %v), want (%q, %v)", tc.comp, tc.key, got, set, tc.want, tc.set)
		}
	}
}
