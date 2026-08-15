// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// ScenarioInfo is the JSON projection of scenarios.Scenario. Kept as a
// separate DTO so the wire format doesn't drift when the internal shape
// changes.
type ScenarioInfo struct {
	ID           string     `json:"id"`
	Root         RootRef    `json:"root"`
	Chain        []StepInfo `json:"chain"`
	Observations []string   `json:"observations,omitempty"`
}

// RootRef identifies the implicated upstream component + state.
type RootRef struct {
	Component string `json:"component"`
	State     string `json:"state"`
}

// StepInfo is one link in a scenario chain.
type StepInfo struct {
	Component   string   `json:"component"`
	State       string   `json:"state"`
	EmitsOnEdge string   `json:"emits_on_edge,omitempty"`
	Observes    []string `json:"observes,omitempty"`
}

// ScenariosListParams is the input for scenarios.list. Enumeration uses
// the model bound to the incident — no separate model_ref needed.
type ScenariosListParams struct {
	IncidentID string `json:"incident_id"`
}

// ScenariosListResult holds the enumerated chains.
type ScenariosListResult struct {
	Scenarios []ScenarioInfo `json:"scenarios"`
}

// ScenariosList returns every failure chain the enumerator produces for
// the model — the unfiltered universe of scenarios the engine can reason
// over. Callers combine this with `scenarios.alive` for a live/eliminated
// split.
func (h *Handler) ScenariosList(p ScenariosListParams) (*ScenariosListResult, error) {
	return withModelContext(p.IncidentID, false, func(_ *incident.Incident, m *model.Model, reg *providersupport.Registry) (*ScenariosListResult, error) {
		return &ScenariosListResult{Scenarios: mapScenarios(scenarios.Enumerate(m, reg))}, nil
	})
}

// ScenariosAliveParams is the input for scenarios.alive.
type ScenariosAliveParams struct {
	IncidentID string `json:"incident_id"`
}

// ScenariosAlive returns the subset of enumerated scenarios still
// consistent with the incident's facts. Contradicted scenarios drop out.
// Unconstrained scenarios stay in — no facts means no eliminations.
func (h *Handler) ScenariosAlive(p ScenariosAliveParams) (*ScenariosListResult, error) {
	return withModelContext(p.IncidentID, false, func(inc *incident.Incident, m *model.Model, reg *providersupport.Registry) (*ScenariosListResult, error) {
		alive := strategy.FilterLive(scenarios.Enumerate(m, reg), inc.Store, m, reg)
		return &ScenariosListResult{Scenarios: mapScenarios(alive)}, nil
	})
}

func mapScenarios(in []scenarios.Scenario) []ScenarioInfo {
	out := make([]ScenarioInfo, 0, len(in))
	for _, s := range in {
		out = append(out, mapScenario(s))
	}
	return out
}

// mapScenario converts a single scenario. Split out so the snapshot's
// eliminated-list doesn't allocate a one-element slice per entry.
func mapScenario(s scenarios.Scenario) ScenarioInfo {
	info := ScenarioInfo{
		ID:    s.ID,
		Root:  RootRef{Component: s.Root.Component, State: s.Root.State},
		Chain: make([]StepInfo, 0, len(s.Chain)),
	}
	for _, step := range s.Chain {
		// Defensive copy of Observes — the returned wire shape must
		// not share backing memory with the source scenarios.Scenario
		// (which may be cached or iterated again by the enumerator).
		var observes []string
		if len(step.Observes) > 0 {
			observes = append([]string(nil), step.Observes...)
		}
		info.Chain = append(info.Chain, StepInfo{
			Component:   step.Component,
			State:       step.State,
			EmitsOnEdge: step.EmitsOnEdge,
			Observes:    observes,
		})
		if len(observes) > 0 {
			info.Observations = observes
		}
	}
	return info
}
