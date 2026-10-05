// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"fmt"
	"strconv"

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
	// Count is how many chains this one stands for when it represents a
	// class (same root, root state and terminal); absent for a raw chain.
	Count int `json:"count,omitempty"`
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

// A listing returns one representative per class of chains (same root,
// root state and terminal), a page at a time. A storefront representative is
// about a kilobyte of JSON, and clients cap a tool result well below what a
// few hundred of them take, so even the classes are paged.
const (
	defaultPageSize = 50
	maxPageSize     = 500
)

// ListParams pages a scenario listing.
type ListParams struct {
	// All lists every chain rather than one representative per class.
	All bool `json:"all,omitempty"`
	// Limit is the page size: 50 by default, at most 500.
	Limit int `json:"limit,omitempty"`
	// PageToken continues a listing from the previous page's
	// next_page_token. A listing is computed per call, so a page token
	// assumes the incident's facts have not changed in between.
	PageToken string `json:"page_token,omitempty"`
}

// ScenariosListParams is the input for scenarios_list. Enumeration uses
// the model bound to the incident — no separate model_ref needed.
type ScenariosListParams struct {
	IncidentID string `json:"incident_id"`
	ListParams
}

// ScenariosListResult is one page of a listing.
type ScenariosListResult struct {
	Scenarios []ScenarioInfo `json:"scenarios"`
	// Chains is how many chains the whole listing covers.
	Chains int `json:"chains"`
	// Total is how many entries the listing has over all its pages:
	// classes, or chains with all.
	Total int `json:"total"`
	// NextPageToken continues the listing; absent on the last page.
	NextPageToken string `json:"next_page_token,omitempty"`
}

// ScenariosList lists the failure chains the enumerator produces for the
// model — the universe of scenarios the engine reasons over. Callers
// combine this with `scenarios_alive` for a live/eliminated split.
func (h *Handler) ScenariosList(p ScenariosListParams) (*ScenariosListResult, error) {
	return withModelContext(p.IncidentID, false, func(_ *incident.Incident, m *model.Model, reg *providersupport.Registry) (*ScenariosListResult, error) {
		return listScenarios(scenarios.Enumerate(m, reg), p.ListParams)
	})
}

// ScenariosAliveParams is the input for scenarios_alive.
type ScenariosAliveParams struct {
	IncidentID string `json:"incident_id"`
	ListParams
}

// ScenariosAlive lists the enumerated scenarios still consistent with the
// incident's facts. Contradicted scenarios drop out. Unconstrained
// scenarios stay in — no facts means no eliminations.
func (h *Handler) ScenariosAlive(p ScenariosAliveParams) (*ScenariosListResult, error) {
	return withModelContext(p.IncidentID, false, func(inc *incident.Incident, m *model.Model, reg *providersupport.Registry) (*ScenariosListResult, error) {
		alive := strategy.FilterLive(scenarios.Enumerate(m, reg), inc.Store, m, reg)
		return listScenarios(alive, p.ListParams)
	})
}

// listScenarios is the page of scs that p asks for: representatives by
// default, chains with p.All.
func listScenarios(scs []scenarios.Scenario, p ListParams) (*ScenariosListResult, error) {
	out := &ScenariosListResult{Chains: len(scs), Scenarios: []ScenarioInfo{}}
	if p.All {
		start, end, next, err := page(len(scs), p.Limit, p.PageToken)
		if err != nil {
			return nil, err
		}
		out.Total, out.NextPageToken = len(scs), next
		out.Scenarios = mapScenarios(scs[start:end])
		return out, nil
	}
	reps := scenarios.Representatives(scs)
	start, end, next, err := page(len(reps), p.Limit, p.PageToken)
	if err != nil {
		return nil, err
	}
	out.Total, out.NextPageToken = len(reps), next
	out.Scenarios = mapRepresentatives(reps[start:end])
	return out, nil
}

// page returns the bounds of the page of an n-entry listing that limit and
// token ask for, and the next page's token ("" on the last page).
func page(n, limit int, token string) (start, end int, next string, err error) {
	switch {
	case limit <= 0:
		limit = defaultPageSize
	case limit > maxPageSize:
		limit = maxPageSize
	}
	if token != "" {
		start, err = strconv.Atoi(token)
		if err != nil || start < 0 || start > n {
			return 0, 0, "", fmt.Errorf("page_token %q is not one this listing issued", token)
		}
	}
	end = min(start+limit, n)
	if end < n {
		next = strconv.Itoa(end)
	}
	return start, end, next, nil
}

func mapRepresentatives(in []scenarios.Representative) []ScenarioInfo {
	out := make([]ScenarioInfo, 0, len(in))
	for _, r := range in {
		info := mapScenario(r.Scenario)
		info.Count = r.Count
		out = append(out, info)
	}
	return out
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
