package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/mgt-tool/mgtt/internal/engine"
	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// IncidentSnapshotParams — Phase 1 has no include/depth selectors;
// snapshot always returns the full bundle. Those knobs land in Phase 2.
type IncidentSnapshotParams struct {
	IncidentID string `json:"incident_id"`
}

// ModelPtr identifies the model an incident is bound to. Hash is a
// sha256 of the file contents at snapshot time — populated opportunistically
// so agents can detect drift when the model was edited between calls.
type ModelPtr struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256,omitempty"`
}

// EliminatedScenarioInfo extends ScenarioInfo with a free-text reason.
type EliminatedScenarioInfo struct {
	ScenarioInfo
	Reason string `json:"reason,omitempty"`
}

// IncidentSnapshotResult is the diagnostic-memory bundle the snapshot
// tool returns. Single object, full disclosure — agents run their own
// summarisation if they want less.
type IncidentSnapshotResult struct {
	IncidentID          string                   `json:"incident_id"`
	ModelRef            ModelPtr                 `json:"model_ref"`
	StartedAt           time.Time                `json:"started_at"`
	EndedAt             *time.Time               `json:"ended_at,omitempty"`
	Status              string                   `json:"status"`
	EntryPoint          string                   `json:"entry_point"`
	SurvivingScenarios  []ScenarioInfo           `json:"surviving_scenarios"`
	EliminatedScenarios []EliminatedScenarioInfo `json:"eliminated_scenarios"`
	Facts               []FactEntry              `json:"facts"`
	SuggestedNext       *SuggestedProbe          `json:"suggested_next,omitempty"`
	Verdict             string                   `json:"verdict,omitempty"`
}

// IncidentSnapshot assembles the full diagnostic memory for an incident
// — scenarios (live vs eliminated), facts, current suggestion, status,
// and a pointer to the bound model.
func (h *Handler) IncidentSnapshot(p IncidentSnapshotParams) (*IncidentSnapshotResult, error) {
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

	out := &IncidentSnapshotResult{
		IncidentID: inc.ID,
		ModelRef:   ModelPtr{Path: inc.ModelRef, Sha256: fileSha256(inc.ModelRef)},
		StartedAt:  inc.Started,
		Status:     statusOf(inc),
		EntryPoint: m.EntryPoint(),
		Verdict:    inc.Verdict,
	}
	if !inc.Ended.IsZero() {
		ended := inc.Ended
		out.EndedAt = &ended
	}

	// Facts — reuse FactsList handler for identical shape.
	if factsResult, err := h.FactsList(FactsListParams{IncidentID: inc.ID}); err == nil {
		out.Facts = factsResult.Facts
	} else {
		out.Facts = []FactEntry{}
	}

	// Scenarios: enumerate once, then split into surviving vs eliminated.
	// FilterLive returns the survivors; the complement is the eliminated
	// set, with a light reason populated when we can compute it cheaply.
	all := scenarios.Enumerate(m, reg)
	alive := strategy.FilterLive(all, inc.Store, m, reg)
	aliveByID := make(map[string]struct{}, len(alive))
	for _, s := range alive {
		aliveByID[s.ID] = struct{}{}
	}
	out.SurvivingScenarios = mapScenarios(alive)
	out.EliminatedScenarios = make([]EliminatedScenarioInfo, 0)
	for _, s := range all {
		if _, ok := aliveByID[s.ID]; ok {
			continue
		}
		out.EliminatedScenarios = append(out.EliminatedScenarios, EliminatedScenarioInfo{
			ScenarioInfo: mapScenarios([]scenarios.Scenario{s})[0],
			Reason:       "contradicted by observed facts",
		})
	}

	// Suggested-next: same engine call `plan` uses. Omitted when the
	// engine has no next move (solved incident or dead-ended).
	tree := engine.Plan(m, reg, inc.Store, m.EntryPoint())
	if tree.Suggested != nil {
		out.SuggestedNext = &SuggestedProbe{
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

func statusOf(inc *incident.Incident) string {
	if inc.Ended.IsZero() {
		return "open"
	}
	return "closed"
}

// fileSha256 returns the hex digest of the file at path, or "" on any
// read failure — the field is declarative, not load-bearing, so silent
// absence beats failing the whole snapshot.
func fileSha256(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
