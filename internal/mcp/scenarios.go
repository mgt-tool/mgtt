package mcp

import (
	"fmt"

	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// ScenarioInfo is the JSON projection of scenarios.Scenario. Kept as a
// separate DTO so the wire format doesn't drift when the internal shape
// changes.
type ScenarioInfo struct {
	ID           string      `json:"id"`
	Root         RootRef     `json:"root"`
	Chain        []StepInfo  `json:"chain"`
	Observations []string    `json:"observations,omitempty"`
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
	if p.IncidentID == "" {
		return nil, fmt.Errorf("incident_id is required")
	}
	mu := lockFor(p.IncidentID)
	mu.RLock()
	defer mu.RUnlock()
	inc, err := incident.LoadByID(p.IncidentID)
	if err != nil {
		return nil, fmt.Errorf("load incident: %w", err)
	}
	if inc.ModelRef == "" {
		return nil, fmt.Errorf("incident %q has no model_ref — was it started via MCP?", p.IncidentID)
	}
	m, reg, err := loadContext(inc.ModelRef)
	if err != nil {
		return nil, err
	}
	enumerated := scenarios.Enumerate(m, reg)
	return &ScenariosListResult{Scenarios: mapScenarios(enumerated)}, nil
}

// ScenariosAliveParams is the input for scenarios.alive.
type ScenariosAliveParams struct {
	IncidentID string `json:"incident_id"`
}

// ScenariosAlive returns the subset of enumerated scenarios still
// consistent with the incident's facts. Contradicted scenarios drop out.
// Unconstrained scenarios stay in — no facts means no eliminations.
func (h *Handler) ScenariosAlive(p ScenariosAliveParams) (*ScenariosListResult, error) {
	if p.IncidentID == "" {
		return nil, fmt.Errorf("incident_id is required")
	}
	mu := lockFor(p.IncidentID)
	mu.RLock()
	defer mu.RUnlock()
	inc, err := incident.LoadByID(p.IncidentID)
	if err != nil {
		return nil, fmt.Errorf("load incident: %w", err)
	}
	if inc.ModelRef == "" {
		return nil, fmt.Errorf("incident %q has no model_ref — was it started via MCP?", p.IncidentID)
	}
	m, reg, err := loadContext(inc.ModelRef)
	if err != nil {
		return nil, err
	}
	alive := strategy.FilterLive(scenarios.Enumerate(m, reg), inc.Store, m, reg)
	return &ScenariosListResult{Scenarios: mapScenarios(alive)}, nil
}

func mapScenarios(in []scenarios.Scenario) []ScenarioInfo {
	out := make([]ScenarioInfo, 0, len(in))
	for _, s := range in {
		info := ScenarioInfo{
			ID:   s.ID,
			Root: RootRef{Component: s.Root.Component, State: s.Root.State},
		}
		info.Chain = make([]StepInfo, 0, len(s.Chain))
		for _, step := range s.Chain {
			info.Chain = append(info.Chain, StepInfo{
				Component:   step.Component,
				State:       step.State,
				EmitsOnEdge: step.EmitsOnEdge,
				Observes:    step.Observes,
			})
			// Terminal step's observed facts surface at the scenario
			// level for agents that want a quick "what to collect next"
			// pointer.
			if len(step.Observes) > 0 {
				info.Observations = step.Observes
			}
		}
		out = append(out, info)
	}
	return out
}
