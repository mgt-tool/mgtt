// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"fmt"
	"maps"
	"slices"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"gopkg.in/yaml.v3"
)

// ResolveType looks up the component's type against the registry, using
// the component's effective providers list. The returned owner is the
// provider short name that satisfied the lookup. Callers that previously
// re-implemented the "pick component.Providers else meta.Providers,
// then reg.ResolveType" dance should use this instead.
//
// The type returned is the component's effective type: the provider's,
// with the model's word for this node on top -- its own states first, its
// healthy_in states marked healthy, its failure_modes merged in -- so
// every reader, from scenario enumeration to the health verdict, sees one
// definition.
func (c *Component) ResolveType(m *Model, reg *providersupport.Registry) (*providersupport.Type, string, error) {
	t, owner, err := reg.ResolveType(c.EffectiveProviders(m), c.Type)
	if err != nil || t == nil || (len(c.States) == 0 && len(c.HealthyIn) == 0 && len(c.FailureModes) == 0) {
		return t, owner, err
	}
	e := *t
	e.States = append(slices.Clone(c.States), t.States...)
	for i, st := range e.States {
		if slices.Contains(c.HealthyIn, st.Name) && st.Verdict != providersupport.VerdictBroken {
			e.States[i].Verdict = providersupport.VerdictHealthy
		}
	}
	e.FailureModes = maps.Clone(t.FailureModes)
	if e.FailureModes == nil {
		e.FailureModes = map[string][]string{}
	}
	for state, labels := range c.FailureModes {
		e.FailureModes[state] = labels
	}
	return &e, owner, nil
}

// compileModelStates reads a component's states: block, in order, each
// one broken by definition; its can_cause labels join the component's
// failure_modes.
func compileModelStates(c *Component, node *yaml.Node) error {
	if node.Kind == 0 {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("states must be a mapping of state name to when:")
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := node.Content[i].Value
		var raw struct {
			When        string   `yaml:"when"`
			Description string   `yaml:"description"`
			CanCause    []string `yaml:"can_cause"`
		}
		if err := node.Content[i+1].Decode(&raw); err != nil {
			return fmt.Errorf("state %q: %w", name, err)
		}
		if raw.When == "" {
			return fmt.Errorf("state %q: when: is required", name)
		}
		when, err := expr.Parse(raw.When)
		if err != nil {
			return fmt.Errorf("state %q: invalid when expression %q: %w", name, raw.When, err)
		}
		c.States = append(c.States, providersupport.StateDef{
			Name: name, WhenRaw: raw.When, When: when, Description: raw.Description,
			Verdict: providersupport.VerdictBroken,
		})
		if len(raw.CanCause) > 0 {
			if c.FailureModes == nil {
				c.FailureModes = map[string][]string{}
			}
			c.FailureModes[name] = append(c.FailureModes[name], raw.CanCause...)
		}
	}
	return nil
}
