// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package simulate — synth: turn scenario step predicates into concrete
// fact bindings seeded into an in-memory store. See fromscenarios.go
// for how RunFromScenarios drives this.
package simulate

import (
	"errors"
	"fmt"

	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// synthesizeFactsForScenario walks the scenario chain and, for each
// step, derives fact values that make its state predicate evaluate true
// and writes them into store.
func synthesizeFactsForScenario(store *facts.Store, m *model.Model, reg *providersupport.Registry, s scenarios.Scenario) error {
	return synthesizeFactsForSteps(store, m, reg, s.Chain)
}

// synthesizeFactsForSteps is the chain-prefix variant used by --fuzz.
// Non-fatal synthesis errors are swallowed — occam falls back to probe
// requests we answer on demand. ErrCrossStateRef, however, bubbles up:
// no probe answer can rescue a predicate that keys off another
// component's derived state.
func synthesizeFactsForSteps(store *facts.Store, m *model.Model, reg *providersupport.Registry, steps []scenarios.Step) error {
	for _, step := range steps {
		if err := synthesizeStep(store, m, reg, step); err != nil {
			if errors.Is(err, ErrCrossStateRef) {
				return err
			}
			// Other errors are not fatal — occam will fall back to
			// asking probes and we answer them on demand.
			continue
		}
	}
	return nil
}

// synthesizeStep finds the StateDef that matches step.State and derives
// fact values that satisfy its When predicate.
func synthesizeStep(store *facts.Store, m *model.Model, reg *providersupport.Registry, step scenarios.Step) error {
	t, err := resolveStepType(m, reg, step.Component)
	if err != nil {
		return err
	}
	for _, st := range t.States {
		if st.Name != step.State {
			continue
		}
		if st.When == nil {
			return nil
		}
		assignments, err := deriveSatisfyingAssignments(st.When, true, t.Facts)
		if err != nil {
			return err
		}
		writeSynthFacts(store, step.Component, assignments, "simulate-synth")
		return nil
	}
	return nil
}

// synthesizeProbeAnswer answers an Occam-requested probe on (component,
// fact). When the component is in the scenario's chain we satisfy the
// predicate of its step-state. Otherwise we write a benign placeholder
// so the strategy treats the component as verified-healthy.
func synthesizeProbeAnswer(store *facts.Store, m *model.Model, reg *providersupport.Registry, s scenarios.Scenario, p *strategy.Probe) error {
	for _, step := range s.Chain {
		if step.Component == p.Component {
			return synthesizeStep(store, m, reg, step)
		}
	}
	store.Append(p.Component, facts.Fact{
		Key:       p.Fact,
		Value:     "synth-default",
		Collector: "simulate-synth",
	})
	return nil
}

// synthesizeActiveStatesForNonChain writes default-active-state facts
// for every component not on s's chain. Removes ambiguity when
// multiple enumerated scenarios touch overlapping components — the
// active-state facts contradict any scenario that includes a
// non-chain component in a non-default state.
func synthesizeActiveStatesForNonChain(store *facts.Store, m *model.Model, reg *providersupport.Registry, s scenarios.Scenario) {
	onChain := map[string]bool{}
	for _, step := range s.Chain {
		onChain[step.Component] = true
	}
	for compName := range m.Components {
		if onChain[compName] {
			continue
		}
		t, err := resolveStepType(m, reg, compName)
		if err != nil || t == nil || t.DefaultActiveState == "" {
			continue
		}
		for _, st := range t.States {
			if st.Name != t.DefaultActiveState || st.When == nil {
				continue
			}
			// Best-effort: cross-state refs in the default-active
			// predicate shouldn't fail the whole scenario, just skip.
			assignments, err := deriveSatisfyingAssignments(st.When, true, t.Facts)
			if err != nil {
				break
			}
			writeSynthFacts(store, compName, assignments, "simulate-synth-active")
			break
		}
	}
}

// writeSynthFacts appends every (key, value) in assignments as a fact
// on component, tagged with the given collector label.
func writeSynthFacts(store *facts.Store, component string, assignments map[string]any, collector string) {
	for k, v := range assignments {
		store.Append(component, facts.Fact{
			Key:       k,
			Value:     v,
			Collector: collector,
		})
	}
}

func resolveStepType(m *model.Model, reg *providersupport.Registry, compName string) (*providersupport.Type, error) {
	comp := m.Components[compName]
	if comp == nil {
		return nil, fmt.Errorf("component %q not in model", compName)
	}
	t, _, err := comp.ResolveType(m, reg)
	if err != nil {
		return nil, fmt.Errorf("resolve type for %q: %w", compName, err)
	}
	if t == nil {
		return nil, fmt.Errorf("type %q unresolved for component %q", comp.Type, compName)
	}
	return t, nil
}
