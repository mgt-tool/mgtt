// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mgt-tool/mgtt/internal/engine"
	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
	probeexec "github.com/mgt-tool/mgtt/internal/providersupport/probe/exec"
)

// ProbeParams carries the inputs for the `probe` tool. Phase 1: no
// probe_id override — the server always picks engine.Plan's current
// suggestion. Agents that want to target a specific component call
// `plan` with a `component` override first.
type ProbeParams struct {
	IncidentID string `json:"incident_id"`
	Component  string `json:"component,omitempty"`
	Execute    bool   `json:"execute"`
}

// ProbeResult conveys the outcome. Status discriminates the shape:
//   - "rendered"                 — execute=false; rendered_command populated
//   - "executed"                 — ran successfully; value + raw populated
//   - "not_found"                — probe ran; resource missing
//   - "operator_prompt_required" — provider declares this fact is operator-sourced
//   - "no_suggestion"            — engine has no next probe (resolved or stuck)
//   - "error"                    — the probe ran and failed; raw may carry stderr
//   - "blocked_readonly"         — --readonly-only active and provider is write-capable
type ProbeResult struct {
	Status          string `json:"status"`
	Component       string `json:"component,omitempty"`
	Fact            string `json:"fact,omitempty"`
	Provider        string `json:"provider,omitempty"`
	RenderedCommand string `json:"rendered_command,omitempty"`
	Value           any    `json:"value,omitempty"`
	Raw             string `json:"raw,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// Probe renders or executes the engine's next-suggested probe.
//
// Writer lock is held even in render-mode: engine.Plan reads the store
// and a concurrent FactAdd could race on the internal maps; taking the
// same lock is cheaper than branching for a handful of allocations.
func (h *Handler) Probe(p ProbeParams) (*ProbeResult, error) {
	return withModelContext(p.IncidentID, true, func(inc *incident.Incident, m *model.Model, reg *providersupport.Registry) (*ProbeResult, error) {
		s := suggestProbe(m, reg, inc, p.Component)
		if s == nil {
			tree := engine.PlanWith(m, reg, inc.Store, entryOrDefault(m, p.Component), strategy.ParseSuspectHints(inc.Store.Meta.Suspects))
			return &ProbeResult{Status: "no_suggestion", Reason: tree.RootCause}, nil
		}
		rendered := probe.Substitute(s.Command, s.Component, s.Vars, nil)
		if !p.Execute {
			return &ProbeResult{Status: "rendered", Component: s.Component, Fact: s.Fact, Provider: s.Provider, RenderedCommand: rendered}, nil
		}
		if s.Command == "" {
			return &ProbeResult{Status: "operator_prompt_required", Component: s.Component, Fact: s.Fact, Provider: s.Provider}, nil
		}
		if gate := h.checkProbeGates(inc, reg, s, rendered); gate != nil {
			return gate, nil
		}
		if err := probe.ValidateCommand(rendered, s.Command); err != nil {
			return blocked("error", err.Error(), s, rendered), nil
		}
		return h.runProbeAndStore(inc, s, rendered)
	})
}

// entryOrDefault returns the override when non-empty else the model's
// declared entry point.
func entryOrDefault(m *model.Model, override string) string {
	if override != "" {
		return override
	}
	return m.EntryPoint()
}

// suggestProbe runs engine.PlanWith and returns the suggested probe
// (or nil when the engine has no next move).
func suggestProbe(m *model.Model, reg *providersupport.Registry, inc *incident.Incident, componentOverride string) *engine.Probe {
	tree := engine.PlanWith(m, reg, inc.Store, entryOrDefault(m, componentOverride), strategy.ParseSuspectHints(inc.Store.Meta.Suspects))
	return tree.Suggested
}

// checkProbeGates evaluates the safety policy stack (readonly-only,
// on-write, budget) in precedence order. Returns a non-nil
// ProbeResult when a gate fires; nil to proceed with execution.
func (h *Handler) checkProbeGates(inc *incident.Incident, reg *providersupport.Registry, s *engine.Probe, rendered string) *ProbeResult {
	writeCapable := !providerIsReadOnly(reg, s.Provider)
	if h.cfg.ReadonlyOnly && writeCapable {
		return blocked("blocked_readonly", "provider is not declared read_only: true", s, rendered)
	}
	if writeCapable {
		switch h.cfg.OnWrite {
		case "fail":
			return blocked("blocked_write_fail", "on_write=fail; write probe refused", s, rendered)
		case "pause":
			return blocked("blocked_write_pause", "on_write=pause; write probe held for review", s, rendered)
		}
	}
	if h.cfg.MaxExecutePerIncident > 0 && probeCollectedFactCount(inc.Store) >= h.cfg.MaxExecutePerIncident {
		return blocked("blocked_budget", fmt.Sprintf("max_execute_per_incident=%d reached", h.cfg.MaxExecutePerIncident), s, rendered)
	}
	return nil
}

// runProbeAndStore executes the rendered probe, writes the resulting
// fact into the store, and builds the wire result.
func (h *Handler) runProbeAndStore(inc *incident.Incident, s *engine.Probe, rendered string) (*ProbeResult, error) {
	ctx := probe.WithTracer(context.Background(), probe.NewTracer())
	res, runErr := probeexec.Default().Run(ctx, probe.Command{
		Raw:       rendered,
		Parse:     s.ParseMode,
		Provider:  s.Provider,
		Component: s.Component,
		Fact:      s.Fact,
		Type:      s.Type,
		Resource:  s.Resource,
		Vars:      s.Vars,
		Timeout:   probeTimeoutFromConfig(h.cfg),
	})
	if runErr != nil {
		return blocked("error", runErr.Error(), s, rendered), nil
	}
	if res.Status == probe.StatusNotFound {
		// Record the not_found sentinel so subsequent plan calls
		// don't re-suggest the same probe. Status (not Note) is what
		// the engine's liveset filter reads.
		inc.Store.Append(s.Component, facts.Fact{
			Key: s.Fact, Collector: "probe",
			At: time.Now().UTC(), Status: facts.FactStatusNotFound,
		})
		_ = inc.Store.Save()
		return &ProbeResult{
			Status: "not_found", Component: s.Component, Fact: s.Fact,
			Provider: s.Provider, RenderedCommand: rendered, Raw: res.Raw,
		}, nil
	}
	inc.Store.Append(s.Component, facts.Fact{
		Key: s.Fact, Value: res.Parsed, Collector: "probe",
		At: time.Now().UTC(), Raw: res.Raw,
	})
	if err := inc.Store.Save(); err != nil {
		return nil, fmt.Errorf("save state: %w", err)
	}
	return &ProbeResult{
		Status: "executed", Component: s.Component, Fact: s.Fact,
		Provider: s.Provider, RenderedCommand: rendered,
		Value: res.Parsed, Raw: res.Raw,
	}, nil
}

// blocked builds a ProbeResult for any non-success terminal state that
// still has a rendered command to surface — six almost-identical struct
// literals were collapsed into one call.
func blocked(status, reason string, s *engine.Probe, rendered string) *ProbeResult {
	return &ProbeResult{
		Status:          status,
		Component:       s.Component,
		Fact:            s.Fact,
		Provider:        s.Provider,
		RenderedCommand: rendered,
		Reason:          reason,
	}
}

// providerIsReadOnly reports whether the registry's copy of the named
// provider declared read_only: true. Unknown providers are treated as
// write-capable (returns false): safer than assuming read-only when the
// --readonly-only gate is active.
func providerIsReadOnly(reg *providersupport.Registry, name string) bool {
	if name == "" {
		return false
	}
	p, ok := reg.Get(name)
	if !ok || p == nil {
		return false
	}
	return p.ReadOnly
}

// probeTimeoutSecondsMax caps the operator-configurable probe timeout.
// Design §7.2: "default 30s, clamped to 5m max." Typos like
// --probe-timeout=3000 must not silently bind the server for ~50 minutes.
const probeTimeoutSecondsMax = 300

// probeTimeoutFromConfig converts the integer-seconds config field into a
// time.Duration. Zero means "use the runner default" (typically 30s).
// Anything over 5 minutes is clamped to 5 minutes.
func probeTimeoutFromConfig(cfg Config) time.Duration {
	if cfg.ProbeTimeoutSeconds <= 0 {
		return 0
	}
	sec := cfg.ProbeTimeoutSeconds
	if sec > probeTimeoutSecondsMax {
		sec = probeTimeoutSecondsMax
	}
	return time.Duration(sec) * time.Second
}

// probeCollectedFactCount counts facts whose collector is "probe" — the
// marker left by actual probe executions. Agent-added facts (collector:
// "agent") don't consume the budget.
func probeCollectedFactCount(store *facts.Store) int {
	if store == nil {
		return 0
	}
	n := 0
	for _, c := range store.AllComponents() {
		for _, f := range store.FactsFor(c) {
			if f.Collector == "probe" {
				n++
			}
		}
	}
	return n
}
