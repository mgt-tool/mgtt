// Package strategy defines probe-selection strategies for the mgtt
// engine. Two built-ins: occam (scenario-based, shortest-first) and
// bfs (graph-traversal fallback). The engine picks between them via
// AutoSelect based on whether scenarios are available.
package strategy

import (
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// Strategy picks the next probe given current state.
type Strategy interface {
	Name() string
	SuggestProbe(in Input) Decision
}

// Input carries everything a strategy needs to pick a probe.
type Input struct {
	Model     *model.Model
	Registry  *providersupport.Registry
	Store     *facts.Store
	Scenarios []scenarios.Scenario // may be empty for bfs
	Suspects  []SuspectHint        // optional operator hints
}

// SuspectHint is one --suspect value. State is optional (empty = any).
type SuspectHint struct {
	Component string
	State     string
}

// Decision is what the strategy returns.
type Decision struct {
	Probe     *Probe              // non-nil when suggesting a probe
	Done      bool                // single scenario remains → root cause found
	RootCause *scenarios.Scenario // set when Done
	Stuck     bool                // no scenarios compatible with collected facts
	Reason    string              // human-readable explanation
}

// metaVars returns Input.Model.Meta.Vars safely (nil-tolerant). Used by
// the strategies to forward vars onto the Probe struct so the runner
// can substitute `{key}` placeholders at probe time.
func metaVars(in Input) map[string]string {
	if in.Model == nil {
		return nil
	}
	return in.Model.Meta.Vars
}

// Probe describes the concrete next probe to run.
type Probe struct {
	Component  string
	Fact       string
	Provider   string
	Type       string            // resolved component type — required by the provider binary
	Cost       string
	Access     string
	Command    string
	ParseMode  string
	Vars       map[string]string // model.meta.vars forwarded for {key} substitution
	Eliminates []string          // scenario IDs this probe would invalidate (display only)
}
