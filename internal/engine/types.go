// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/state"
)

// PathTree is the output of the Plan function — the complete analysis of
// failure paths from an entry point through the dependency graph.
type PathTree struct {
	Entry      string
	Paths      []Path
	Eliminated []Path
	Suggested  *Probe
	RootCause  string
	States     *state.Derivation
	// CannotRuleOut names the components the facts left Unknown because
	// some could not be read. RootCause is only as good as what was seen.
	CannotRuleOut []strategy.Unseen
}

// Path represents a single failure path through the dependency graph.
type Path struct {
	ID         string
	Components []string
	Reason     string // why eliminated, if eliminated
}

// Probe is the engine-level alias for strategy.Probe. Callers that
// previously consumed engine.Probe now see the full strategy-level
// struct (including Type, Resource, Vars) instead of a projection that
// dropped half the fields and forced feature-envy reach-back into
// m.Components to recover them.
type Probe = strategy.Probe
