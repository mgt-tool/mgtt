// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"time"

	"github.com/mgt-tool/mgtt/internal/engine"
	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
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
	return withModelContext(p.IncidentID, false, func(inc *incident.Incident, m *model.Model, reg *providersupport.Registry) (*IncidentSnapshotResult, error) {
		out := &IncidentSnapshotResult{
			IncidentID: inc.ID,
			ModelRef:   ModelPtr{Path: inc.ModelRef, Sha256: fileSha256(inc.ModelRef)},
			StartedAt:  inc.Started,
			Status:     statusOf(inc),
			EntryPoint: m.EntryPoint(),
			Verdict:    inc.Verdict,
			Facts:      storeFactsAsEntries(inc.Store, ""),
		}
		if !inc.Ended.IsZero() {
			ended := inc.Ended
			out.EndedAt = &ended
		}

		// Scenarios: enumerate once, split into surviving vs eliminated.
		// FilterLive returns the survivors; the complement gets a light
		// reason so agents can tell a contradiction from a dead branch.
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
				ScenarioInfo: mapScenario(s),
				Reason:       "contradicted by observed facts",
			})
		}

		// Suggested-next: same engine call `plan` uses.
		tree := engine.PlanWith(m, reg, inc.Store, m.EntryPoint(), strategy.ParseSuspectHints(inc.Store.Meta.Suspects))
		out.SuggestedNext = toSuggested(tree.Suggested, m)
		return out, nil
	})
}

func statusOf(inc *incident.Incident) string {
	if inc.Ended.IsZero() {
		return "open"
	}
	return "closed"
}

// fileSha256 returns the hex digest of the file at path, or "" on any
// read failure — the field is declarative, not load-bearing, so silent
// absence beats failing the whole snapshot. Read failures still emit a
// stderr log when MGTT_DEBUG is set so operators have a crumb when they
// see an unexpected empty sha256.
func fileSha256(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.Getenv("MGTT_DEBUG") != "" {
			log.Printf("mcp snapshot: sha256 read failed for %q: %v", path, err)
		}
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
