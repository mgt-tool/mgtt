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

// snapshotClasses caps each scenario list in a snapshot. The lists hold
// representatives, one per class, with the totals beside them;
// scenarios_alive and scenarios_list page through every class.
const snapshotClasses = 25

// IncidentSnapshotResult is the diagnostic-memory bundle the snapshot
// tool returns, in one object.
type IncidentSnapshotResult struct {
	IncidentID string     `json:"incident_id"`
	ModelRef   ModelPtr   `json:"model_ref"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	Status     string     `json:"status"`
	EntryPoint string     `json:"entry_point"`
	// SurvivingScenarios and EliminatedScenarios are the first 25
	// representatives of each side, each with the count of chains it
	// stands for. The counts below say how much the lists leave out.
	SurvivingScenarios  []ScenarioInfo           `json:"surviving_scenarios"`
	EliminatedScenarios []EliminatedScenarioInfo `json:"eliminated_scenarios"`
	SurvivingChains     int                      `json:"surviving_chains"`
	SurvivingClasses    int                      `json:"surviving_classes"`
	EliminatedChains    int                      `json:"eliminated_chains"`
	EliminatedClasses   int                      `json:"eliminated_classes"`
	Facts               []FactEntry              `json:"facts"`
	SuggestedNext       *SuggestedProbe          `json:"suggested_next,omitempty"`
	Verdict             string                   `json:"verdict,omitempty"`
	CannotRuleOut       []UnseenInfo             `json:"cannot_rule_out,omitempty"`
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

		// Scenarios: classes of the surviving and the eliminated chains,
		// from the graph, so a deep model does not list its chains.
		surviving, gone := classes(m, reg, inc.Store)
		out.SurvivingChains, out.SurvivingClasses = chainsIn(surviving), len(surviving)
		out.EliminatedChains, out.EliminatedClasses = chainsIn(gone), len(gone)
		out.SurvivingScenarios = mapRepresentatives(surviving[:min(len(surviving), snapshotClasses)])
		out.EliminatedScenarios = make([]EliminatedScenarioInfo, 0)
		for _, r := range gone[:min(len(gone), snapshotClasses)] {
			info := mapScenario(r.Scenario)
			info.Count = r.Count
			out.EliminatedScenarios = append(out.EliminatedScenarios, EliminatedScenarioInfo{
				ScenarioInfo: info,
				Reason:       "contradicted by observed facts",
			})
		}

		// Suggested-next: same engine call `plan` uses.
		tree := engine.PlanWith(m, reg, inc.Store, m.EntryPoint(), strategy.ParseSuspectHints(inc.Store.Meta.Suspects))
		out.SuggestedNext = toSuggested(tree.Suggested, m)
		out.CannotRuleOut = mapUnseen(tree.CannotRuleOut)
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
