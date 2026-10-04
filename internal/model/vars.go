// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// VarLookup resolves the variables a type's expressions read, such as the
// per-component threshold in `restart_count <= max_restart_count`, for
// evaluation. reg may be nil, which skips provider defaults.
func (m *Model) VarLookup(reg *providersupport.Registry) expr.VarLookup {
	return varLookup{m: m, reg: reg}
}

type varLookup struct {
	m   *Model
	reg *providersupport.Registry
}

// LookupVar resolves key for component, first match wins: the
// component's vars:, then the model's meta.vars:, then the default its
// provider declares under variables:.
func (v varLookup) LookupVar(component, key string) (string, bool) {
	if v.m == nil {
		return "", false
	}
	comp := v.m.Components[component]
	if comp != nil {
		if val, ok := comp.Vars[key]; ok {
			return val, true
		}
	}
	if val, ok := v.m.Meta.Vars[key]; ok {
		return val, true
	}
	if comp == nil || v.reg == nil {
		return "", false
	}
	_, providerName, err := comp.ResolveType(v.m, v.reg)
	if err != nil {
		return "", false
	}
	p, ok := v.reg.Get(providerName)
	if !ok || p == nil {
		return "", false
	}
	if decl, ok := p.Variables[key]; ok && decl.Default != "" {
		return decl.Default, true
	}
	return "", false
}
