package mcp

import (
	"fmt"

	"github.com/mgt-tool/mgtt/internal/engine"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
)

// PlanParams is the input for `plan`. Only incident_id is required;
// component overrides the entry point the walk starts from.
type PlanParams struct {
	IncidentID string `json:"incident_id"`
	Component  string `json:"component,omitempty"`
}

// PathInfo is the JSON projection of engine.Path — used for both surviving
// and eliminated paths in a plan response.
type PathInfo struct {
	ID         string   `json:"id"`
	Components []string `json:"components"`
	Reason     string   `json:"reason,omitempty"`
}

// SuggestedProbe is the next-probe hint. Command is rendered (vars
// substituted) but not executed — callers invoke `probe` to act on it.
type SuggestedProbe struct {
	Component       string   `json:"component"`
	Fact            string   `json:"fact"`
	Provider        string   `json:"provider,omitempty"`
	Eliminates      []string `json:"eliminates,omitempty"`
	Cost            string   `json:"cost,omitempty"`
	Access          string   `json:"access,omitempty"`
	RenderedCommand string   `json:"rendered_command,omitempty"`
}

// PlanResult is the shape a `plan` tool call returns.
type PlanResult struct {
	Entry      string          `json:"entry"`
	Paths      []PathInfo      `json:"paths"`
	Eliminated []PathInfo      `json:"eliminated,omitempty"`
	Suggested  *SuggestedProbe `json:"suggested,omitempty"`
	RootCause  string          `json:"root_cause,omitempty"`
}

// Plan returns the engine's current assessment of the incident — path
// tree, eliminated paths, and a suggested next probe. Does not execute.
func (h *Handler) Plan(p PlanParams) (*PlanResult, error) {
	if p.IncidentID == "" {
		return nil, fmt.Errorf("incident_id is required")
	}
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

	entry := p.Component
	if entry == "" {
		entry = m.EntryPoint()
	}
	tree := engine.Plan(m, reg, inc.Store, entry)

	out := &PlanResult{
		Entry:      tree.Entry,
		Paths:      mapPaths(tree.Paths),
		Eliminated: mapPaths(tree.Eliminated),
		RootCause:  tree.RootCause,
	}
	if tree.Suggested != nil {
		out.Suggested = &SuggestedProbe{
			Component:       tree.Suggested.Component,
			Fact:            tree.Suggested.Fact,
			Provider:        tree.Suggested.Provider,
			Eliminates:      tree.Suggested.Eliminates,
			Cost:            tree.Suggested.Cost,
			Access:          tree.Suggested.Access,
			RenderedCommand: probe.Substitute(tree.Suggested.Command, tree.Suggested.Component, m.Meta.Vars, nil),
		}
	}
	return out, nil
}

// mapPaths converts engine.Path slices into the JSON-oriented PathInfo shape.
// Always returns a non-nil slice so downstream JSON is a stable "[]" not null.
func mapPaths(in []engine.Path) []PathInfo {
	out := make([]PathInfo, 0, len(in))
	for _, p := range in {
		out = append(out, PathInfo{
			ID:         p.ID,
			Components: p.Components,
			Reason:     p.Reason,
		})
	}
	return out
}
