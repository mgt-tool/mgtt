// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"github.com/mgt-tool/mgtt/internal/engine"
	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
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
	return withModelContext(p.IncidentID, false, func(inc *incident.Incident, m *model.Model, reg *providersupport.Registry) (*PlanResult, error) {
		entry := p.Component
		if entry == "" {
			entry = m.EntryPoint()
		}
		tree := engine.PlanWith(m, reg, inc.Store, entry, strategy.ParseSuspectHints(inc.Store.Meta.Suspects))
		return &PlanResult{
			Entry:      tree.Entry,
			Paths:      mapPaths(tree.Paths),
			Eliminated: mapPaths(tree.Eliminated),
			RootCause:  tree.RootCause,
			Suggested:  toSuggested(tree.Suggested, m),
		}, nil
	})
}

// toSuggested renders an engine.Probe into the wire shape, substituting
// vars at render time. Returns nil when the engine has no next move.
// p.Vars already holds the merged meta+component scope — no need for
// the caller to re-merge from m.Meta.Vars.
func toSuggested(p *engine.Probe, _ *model.Model) *SuggestedProbe {
	if p == nil {
		return nil
	}
	return &SuggestedProbe{
		Component:       p.Component,
		Fact:            p.Fact,
		Provider:        p.Provider,
		Eliminates:      p.Eliminates,
		Cost:            p.Cost,
		Access:          p.Access,
		RenderedCommand: probe.Substitute(p.Command, p.Component, p.Vars, nil),
	}
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
