// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// HealthyRules returns c's effective healthy rules against its type t:
// the type's when c sets none, c's own when it replaces them (a bare list
// or replace:), and the type's followed by c's own for add:.
func (c *Component) HealthyRules(t *providersupport.Type) []expr.Node {
	return effective(c, c.Healthy, typeRules(t))
}

// HealthyRulesRaw is HealthyRules as the rule strings were written.
func (c *Component) HealthyRulesRaw(t *providersupport.Type) []string {
	var typ []string
	if t != nil {
		typ = t.HealthyRaw
	}
	return effective(c, c.HealthyRaw, typ)
}

func typeRules(t *providersupport.Type) []expr.Node {
	if t == nil {
		return nil
	}
	return t.Healthy
}

func effective[R any](c *Component, own, typ []R) []R {
	switch {
	case len(own) == 0:
		return typ
	case c.HealthyMode == "add":
		return append(append([]R(nil), typ...), own...)
	default:
		return own
	}
}
