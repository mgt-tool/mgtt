package mcp

import (
	"fmt"
	"time"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
)

// FactAddParams is the input for fact.add. All four identity fields are
// required; note is optional.
type FactAddParams struct {
	IncidentID string `json:"incident_id"`
	Component  string `json:"component"`
	Key        string `json:"key"`
	Value      any    `json:"value"`
	Note       string `json:"note,omitempty"`
}

// FactAddResult confirms the append happened.
type FactAddResult struct {
	Appended bool `json:"appended"`
}

// FactAdd appends an observation to an incident's store and persists it.
// The MCP "collector" is always "agent" for facts added over the wire —
// distinguishes operator/agent overrides from real probe output.
func (h *Handler) FactAdd(p FactAddParams) (*FactAddResult, error) {
	if p.IncidentID == "" {
		return nil, fmt.Errorf("incident_id is required")
	}
	if p.Component == "" {
		return nil, fmt.Errorf("component is required")
	}
	if p.Key == "" {
		return nil, fmt.Errorf("key is required")
	}
	// Serialize mutations per-incident — concurrent Append+Save without
	// this races on facts.Store's internal maps.
	mu := lockFor(p.IncidentID)
	mu.Lock()
	defer mu.Unlock()
	inc, err := incident.LoadByID(p.IncidentID)
	if err != nil {
		return nil, fmt.Errorf("load incident: %w", err)
	}
	f := facts.Fact{
		Key:       p.Key,
		Value:     p.Value,
		Collector: "agent",
		At:        time.Now().UTC(),
		Note:      p.Note,
	}
	if err := inc.Store.AppendAndSave(p.Component, f); err != nil {
		return nil, fmt.Errorf("append fact: %w", err)
	}
	return &FactAddResult{Appended: true}, nil
}

// storeFactsAsEntries maps a store's facts into FactEntry rows, honoring
// an optional component filter. Shared by FactsList and IncidentSnapshot
// so the snapshot doesn't re-load the state file (design §8.2 race).
func storeFactsAsEntries(store *facts.Store, componentFilter string) []FactEntry {
	out := []FactEntry{}
	if store == nil {
		return out
	}
	for _, c := range store.AllComponents() {
		if componentFilter != "" && c != componentFilter {
			continue
		}
		for _, f := range store.FactsFor(c) {
			out = append(out, FactEntry{
				Component: c,
				Key:       f.Key,
				Value:     f.Value,
				At:        f.At,
				Collector: f.Collector,
				Note:      f.Note,
			})
		}
	}
	return out
}

// FactsListParams filters the listing. Empty Component returns all facts.
type FactsListParams struct {
	IncidentID string `json:"incident_id"`
	Component  string `json:"component,omitempty"`
}

// FactEntry is one row in the facts.list response. Uses plain field types
// for stable JSON shape across SDK versions.
type FactEntry struct {
	Component string    `json:"component"`
	Key       string    `json:"key"`
	Value     any       `json:"value"`
	At        time.Time `json:"at"`
	Collector string    `json:"collector"`
	Note      string    `json:"note,omitempty"`
}

// FactsListResult wraps the flat fact list.
type FactsListResult struct {
	Facts []FactEntry `json:"facts"`
}

// FactsList returns all facts recorded for an incident, optionally
// filtered to one component.
func (h *Handler) FactsList(p FactsListParams) (*FactsListResult, error) {
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
	return &FactsListResult{Facts: storeFactsAsEntries(inc.Store, p.Component)}, nil
}
