// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package strategy defines probe-selection strategies for the mgtt
// engine. Two built-ins: occam (scenario-based, shortest-first) and
// bfs (graph-traversal fallback). The engine picks between them via
// AutoSelect based on whether scenarios are available.
package strategy

import (
	"strings"

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

// ParseSuspectHints turns raw suspect entries into structured hints.
// Accepted shapes (first match wins, to allow unambiguous parsing for
// component names that themselves contain a dot):
//
//   - "component"                     → Component only
//   - "component/state"               → slash-split (preferred)
//   - "component=state"               → equals-split
//   - "component.state"               → dot-split (legacy, back-compat)
//
// Slash and equals never appear in component names (validated at model
// load), so they disambiguate cleanly. Dot-split is kept as the final
// fallback so existing scripts / CLI help examples continue to work;
// authors whose component names contain dots should use "/" or "=".
// Empty / whitespace-only entries are skipped.
func ParseSuspectHints(raw []string) []SuspectHint {
	var out []SuspectHint
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		out = append(out, parseOneSuspect(entry))
	}
	return out
}

func parseOneSuspect(entry string) SuspectHint {
	// "/" and "=" are unambiguous — component names can't contain them.
	for _, sep := range []byte{'/', '='} {
		if i := strings.IndexByte(entry, sep); i >= 0 {
			return SuspectHint{Component: entry[:i], State: entry[i+1:]}
		}
	}
	// Legacy dot-split. "api.crash_looping.extra" pins state to
	// "crash_looping.extra" — documented trade-off.
	if dot := strings.IndexByte(entry, '.'); dot >= 0 {
		return SuspectHint{Component: entry[:dot], State: entry[dot+1:]}
	}
	return SuspectHint{Component: entry}
}

// Decision is what the strategy returns.
type Decision struct {
	Probe     *Probe              // non-nil when suggesting a probe
	Done      bool                // single scenario remains → root cause found
	RootCause *scenarios.Scenario // set when Done
	Stuck     bool                // no scenarios compatible with collected facts
	Reason    string              // human-readable explanation
}

// componentVars returns the effective var map for a component: a copy
// of meta.vars with the component's own Vars merged on top
// (component wins on key collision). Safe to call with empty or nil
// maps. Used when constructing a Probe so the probe runner receives
// per-component overrides (e.g. namespace, region) rather than the
// model-wide default.
func componentVars(in Input, componentName string) map[string]string {
	if in.Model == nil {
		return nil
	}
	return mergeVars(in.Model.Meta.Vars, componentVarsFromModel(in.Model, componentName))
}

func componentVarsFromModel(m *model.Model, componentName string) map[string]string {
	if m == nil {
		return nil
	}
	c := m.Components[componentName]
	if c == nil {
		return nil
	}
	return c.Vars
}

// mergeVars merges two var maps with the second argument winning on
// key collision. Returns nil if both are empty.
func mergeVars(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

// Probe describes the concrete next probe to run.
type Probe struct {
	Component  string
	Fact       string
	Provider   string
	Type       string // resolved component type — required by the provider binary
	Resource   string // upstream resource id; empty = fall back to Component
	Cost       string
	Access     string
	Command    string
	ParseMode  string
	Vars       map[string]string // model.meta.vars forwarded for {key} substitution
	Eliminates []string          // scenario IDs this probe would invalidate (display only)
}
