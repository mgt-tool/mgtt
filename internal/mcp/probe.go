package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mgt-tool/mgtt/internal/engine"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
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
func (h *Handler) Probe(p ProbeParams) (*ProbeResult, error) {
	if p.IncidentID == "" {
		return nil, fmt.Errorf("incident_id is required")
	}
	// Serialize per-incident. Execute-mode appends to the fact store
	// and saves; render-mode only reads the store but takes the same
	// lock (cheaper than branching for a handful of allocations, and
	// guards against a concurrent FactAdd racing on the facts map
	// during engine.Plan's read).
	mu := lockFor(p.IncidentID)
	mu.Lock()
	defer mu.Unlock()
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
	if tree.Suggested == nil {
		return &ProbeResult{Status: "no_suggestion", Reason: tree.RootCause}, nil
	}
	s := tree.Suggested

	rendered := probe.Substitute(s.Command, s.Component, m.Meta.Vars, nil)

	if !p.Execute {
		return &ProbeResult{
			Status:          "rendered",
			Component:       s.Component,
			Fact:            s.Fact,
			Provider:        s.Provider,
			RenderedCommand: rendered,
		}, nil
	}

	// Operator-prompt probes (empty cmd — e.g. generic.component's
	// operator_says_healthy) can't run on the wire. Surface a specific
	// status so the agent knows to gather the fact another way
	// (fact.add with collector="agent").
	if s.Command == "" {
		return &ProbeResult{
			Status:    "operator_prompt_required",
			Component: s.Component,
			Fact:      s.Fact,
			Provider:  s.Provider,
		}, nil
	}

	writeCapable := !providerIsReadOnly(reg, s.Provider)

	// Safety gates, in precedence order. Readonly_only is strictest
	// (applies to any write probe); on_write is a narrower policy used
	// when readonly_only is not set. Both surface the rendered command
	// so downstream agents can still act on the decision.
	if h.cfg.ReadonlyOnly && writeCapable {
		return &ProbeResult{
			Status:          "blocked_readonly",
			Component:       s.Component,
			Fact:            s.Fact,
			Provider:        s.Provider,
			RenderedCommand: rendered,
			Reason:          "provider is not declared read_only: true",
		}, nil
	}
	if writeCapable {
		switch h.cfg.OnWrite {
		case "fail":
			return &ProbeResult{
				Status:          "blocked_write_fail",
				Component:       s.Component,
				Fact:            s.Fact,
				Provider:        s.Provider,
				RenderedCommand: rendered,
				Reason:          "on_write=fail; write probe refused",
			}, nil
		case "pause":
			// Phase 1 has no pause/resume state machine; surface a
			// distinct status so agents can tell pause from fail and
			// route to a human-approval queue.
			return &ProbeResult{
				Status:          "blocked_write_pause",
				Component:       s.Component,
				Fact:            s.Fact,
				Provider:        s.Provider,
				RenderedCommand: rendered,
				Reason:          "on_write=pause; write probe held for review",
			}, nil
		}
	}

	// Budget — count prior probe-collected facts in this incident and
	// refuse once the configured ceiling is reached. Zero means unlimited
	// (Go zero value, preserves capability-by-default).
	if h.cfg.MaxExecutePerIncident > 0 {
		if probeCollectedFactCount(inc.Store) >= h.cfg.MaxExecutePerIncident {
			return &ProbeResult{
				Status:          "blocked_budget",
				Component:       s.Component,
				Fact:            s.Fact,
				Provider:        s.Provider,
				RenderedCommand: rendered,
				Reason:          fmt.Sprintf("max_execute_per_incident=%d reached", h.cfg.MaxExecutePerIncident),
			}, nil
		}
	}

	if err := probe.ValidateCommand(rendered, s.Command); err != nil {
		return &ProbeResult{
			Status:          "error",
			Component:       s.Component,
			Fact:            s.Fact,
			Provider:        s.Provider,
			RenderedCommand: rendered,
			Reason:          err.Error(),
		}, nil
	}

	// Execute via the default shell executor. Image/external runners
	// depend on provider-install metadata that isn't wired through in
	// Phase 1 — covered in a follow-up when real providers exercise MCP.
	comp := m.Components[s.Component]
	compType := ""
	compResource := ""
	if comp != nil {
		compType = comp.Type
		compResource = comp.Resource
	}
	ctx := probe.WithTracer(context.Background(), probe.NewTracer())
	res, runErr := probeexec.Default().Run(ctx, probe.Command{
		Raw:       rendered,
		Parse:     s.ParseMode,
		Provider:  s.Provider,
		Component: s.Component,
		Fact:      s.Fact,
		Type:      compType,
		Resource:  compResource,
		Vars:      m.Meta.Vars,
		Timeout:   probeTimeoutFromConfig(h.cfg),
	})
	if runErr != nil {
		return &ProbeResult{
			Status:          "error",
			Component:       s.Component,
			Fact:            s.Fact,
			Provider:        s.Provider,
			RenderedCommand: rendered,
			Reason:          runErr.Error(),
		}, nil
	}

	if res.Status == probe.StatusNotFound {
		// Record the not_found sentinel so subsequent plan calls don't
		// suggest the same probe.
		inc.Store.Append(s.Component, facts.Fact{
			Key:       s.Fact,
			Value:     nil,
			Collector: "probe",
			At:        time.Now().UTC(),
			Note:      "not_found",
		})
		_ = inc.Store.Save()
		return &ProbeResult{
			Status:          "not_found",
			Component:       s.Component,
			Fact:            s.Fact,
			Provider:        s.Provider,
			RenderedCommand: rendered,
			Raw:             res.Raw,
		}, nil
	}

	inc.Store.Append(s.Component, facts.Fact{
		Key:       s.Fact,
		Value:     res.Parsed,
		Collector: "probe",
		At:        time.Now().UTC(),
		Raw:       res.Raw,
	})
	if err := inc.Store.Save(); err != nil {
		return nil, fmt.Errorf("save state: %w", err)
	}

	return &ProbeResult{
		Status:          "executed",
		Component:       s.Component,
		Fact:            s.Fact,
		Provider:        s.Provider,
		RenderedCommand: rendered,
		Value:           res.Parsed,
		Raw:             res.Raw,
	}, nil
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
