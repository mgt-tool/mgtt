// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package simulate — fromscenarios: iterate every enumerated Scenario
// and assert the occam strategy converges on its declared root.
//
// Approach: synthesize facts that make each step of the chain evaluate
// true under its type's state.When predicate, seed an in-memory fact
// store, and then run Occam repeatedly. The strategy should converge
// (Done) with the root matching the scenario's declared root. When
// Occam asks for more probes, we satisfy them on the fly from the same
// scenario chain.
//
// Predicate-to-fact synthesis: the walker handles the canonical shapes
// that `mgtt model validate` accepts on disk — equality/inequality,
// numeric comparisons against literals or against another fact-name on
// the RHS, and AND/OR composition (OR prefers the branch whose
// bindings don't conflict with the synthesis-in-progress map).
//
// Unsupported: `state == "..."` clauses that reference another
// component's derived state cannot be satisfied by writing a fact —
// the synthesizer surfaces these as an explicit error so the caller
// can distinguish synthesizer limitations from strategy bugs.
package simulate

import (
	"errors"
	"fmt"

	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// ErrCrossStateRef signals the synthesizer hit a `state == "..."` or
// `<component>.state == "..."` shape. It can't satisfy such a
// predicate by writing a fact (the state is derived downstream). The
// caller surfaces this as a FAIL with an explicit "unsupported
// predicate shape" note rather than a mysterious Occam-Stuck result.
var ErrCrossStateRef = errors.New("synthesize: unsupported predicate shape: cross-state reference on component.state")

// RunFromScenarios iterates every scenario and runs the occam strategy
// in a loop to completion (Done or Stuck), asserting the root matches.
// Returns (passed, failed, details). details is per-scenario result
// lines for the caller to print.
func RunFromScenarios(m *model.Model, reg *providersupport.Registry, scs []scenarios.Scenario) (passed, failed int, details []string) {
	for _, s := range scs {
		ok, detail := runOneScenario(m, reg, scs, s)
		if ok {
			passed++
		} else {
			failed++
		}
		details = append(details, detail)
	}
	return
}

func runOneScenario(m *model.Model, reg *providersupport.Registry, all []scenarios.Scenario, s scenarios.Scenario) (bool, string) {
	store := facts.NewInMemory()
	if err := synthesizeFactsForScenario(store, m, reg, s); err != nil {
		if errors.Is(err, ErrCrossStateRef) {
			return false, fmt.Sprintf("%s: FAIL — %v", s.ID, err)
		}
		return false, fmt.Sprintf("%s: FAIL — synthesize facts: %v", s.ID, err)
	}
	// For each component not on the target chain, pin it to its
	// default-active state so competing scenarios whose chains include
	// that component get eliminated (their non-default state predicate
	// is contradicted by the active-state facts).
	synthesizeActiveStatesForNonChain(store, m, reg, s)

	in := strategy.Input{Model: m, Registry: reg, Store: store, Scenarios: all}
	for i := 0; i < 50; i++ {
		d := strategy.Occam().SuggestProbe(in)
		if done, result := checkScenarioDecision(store, m, reg, s, d); done {
			return result.ok, result.msg
		}
	}
	return false, fmt.Sprintf("%s: FAIL — did not converge in 50 iterations", s.ID)
}

type scenarioResult struct {
	ok  bool
	msg string
}

// checkScenarioDecision interprets one Occam decision. Returns done=true
// when the outer loop should stop (success, failure, or stuck). Returns
// done=false when the strategy still wants probes and we synthesised an
// answer into the store for the next iteration.
func checkScenarioDecision(store *facts.Store, m *model.Model, reg *providersupport.Registry, s scenarios.Scenario, d strategy.Decision) (bool, scenarioResult) {
	if d.Done {
		if d.RootCause == nil ||
			d.RootCause.Root.Component != s.Root.Component ||
			d.RootCause.Root.State != s.Root.State {
			got := "nil"
			if d.RootCause != nil {
				got = fmt.Sprintf("%s.%s", d.RootCause.Root.Component, d.RootCause.Root.State)
			}
			return true, scenarioResult{false, fmt.Sprintf("%s: FAIL — want root %s.%s; got %s",
				s.ID, s.Root.Component, s.Root.State, got)}
		}
		return true, scenarioResult{true, fmt.Sprintf("%s: PASS", s.ID)}
	}
	if d.Stuck {
		return true, scenarioResult{false, fmt.Sprintf("%s: FAIL — Stuck: %s", s.ID, d.Reason)}
	}
	if d.Probe == nil {
		return true, scenarioResult{false, fmt.Sprintf("%s: FAIL — no probe, no done/stuck", s.ID)}
	}
	if err := synthesizeProbeAnswer(store, m, reg, s, d.Probe); err != nil {
		return true, scenarioResult{false, fmt.Sprintf("%s: FAIL — synthesize probe answer: %v", s.ID, err)}
	}
	return false, scenarioResult{}
}

// deriveSatisfyingAssignments walks the compiled expression AST and
// returns fact-key→value assignments that make it evaluate to want.
//
// knownFacts lets the walker distinguish a fact-reference RHS like
// `ready_replicas < desired_replicas` (where `desired_replicas` is a
// string token that matches a fact name in the same type) from a
// plain string literal compared via equality. When RHS is a fact
// reference the walker picks an integer pair that satisfies the
// comparison and binds both sides.
//
// Handles:
//   - CmpNode with int/float/bool/string literal on the RHS
//   - CmpNode with a fact-name RHS (ready_replicas < desired_replicas)
//   - AndNode: recurse both sides with the same `want`
//   - OrNode: prefers whichever branch's bindings don't conflict with
//     the synthesis-in-progress map; if both conflict, left wins
//
// Returns ErrCrossStateRef when any CmpNode's LHS keys off
// component.state (a derived-state reference the synthesizer cannot
// satisfy by writing a fact).
func deriveSatisfyingAssignments(node expr.Node, want bool, knownFacts map[string]*providersupport.FactSpec) (map[string]any, error) {
	out := map[string]any{}
	if err := assignFromNode(node, want, out, knownFacts); err != nil {
		return nil, err
	}
	return out, nil
}

func assignFromNode(node expr.Node, want bool, out map[string]any, knownFacts map[string]*providersupport.FactSpec) error {
	// The parser emits pointer variants only. Value-variant cases were
	// unreachable defensive code and were removed.
	switch n := node.(type) {
	case *expr.CmpNode:
		return assignFromCmp(*n, want, out, knownFacts)
	case *expr.AndNode:
		if err := assignFromNode(n.L, want, out, knownFacts); err != nil {
			return err
		}
		return assignFromNode(n.R, want, out, knownFacts)
	case *expr.OrNode:
		// Try both branches on fresh scratch maps and pick whichever
		// doesn't conflict with bindings already in `out`. If both
		// conflict (or neither binds anything), fall back to left.
		leftScratch, rightScratch := map[string]any{}, map[string]any{}
		leftCross := errors.Is(assignFromNode(n.L, want, leftScratch, knownFacts), ErrCrossStateRef)
		rightCross := errors.Is(assignFromNode(n.R, want, rightScratch, knownFacts), ErrCrossStateRef)
		leftOK := !leftCross && len(leftScratch) > 0
		rightOK := !rightCross && len(rightScratch) > 0
		leftConflict := leftOK && bindingsConflict(leftScratch, out)
		rightConflict := rightOK && bindingsConflict(rightScratch, out)

		switch {
		case leftOK && !leftConflict:
			mergeInto(out, leftScratch)
		case rightOK && !rightConflict:
			mergeInto(out, rightScratch)
		case leftOK:
			mergeInto(out, leftScratch)
		case rightOK:
			mergeInto(out, rightScratch)
		case leftCross && rightCross:
			return ErrCrossStateRef
		}
		return nil
	}
	return nil
}

// assignFromCmp handles a single CmpNode. Binds facts from both LHS
// and (when fact-referenced) RHS. Returns ErrCrossStateRef when LHS
// references a derived state.
func assignFromCmp(n expr.CmpNode, want bool, out map[string]any, knownFacts map[string]*providersupport.FactSpec) error {
	if n.Fact == "" {
		return nil
	}
	if n.Fact == "state" {
		return ErrCrossStateRef
	}

	// 1a. Fact-on-RHS: detect when n.Value is a string that names
	// another fact in the same type. Pick an integer pair satisfying
	// the comparison and bind both.
	if rhsName, isFactRef := factRefValue(n.Value, knownFacts, n.Fact); isFactRef {
		lv, rv, ok := satisfyCmpBothFactRefs(n.Op, want)
		if !ok {
			return nil
		}
		if _, exists := out[n.Fact]; !exists {
			out[n.Fact] = lv
		}
		if _, exists := out[rhsName]; !exists {
			out[rhsName] = rv
		}
		return nil
	}

	v, ok := satisfyCmp(n, want)
	if !ok {
		return nil
	}
	// Don't overwrite a prior binding for the same fact — first
	// clause wins. A later conflicting clause just means the
	// predicate is unsatisfiable and our synthesizer cannot help
	// with this scenario; that's reported upstream as a "Stuck".
	if _, exists := out[n.Fact]; !exists {
		out[n.Fact] = v
	}
	return nil
}

// factRefValue returns (name, true) when v is a string that matches a
// fact name in knownFacts (excluding selfFact, which is the LHS and
// would compare a fact against itself — treat that as a literal).
func factRefValue(v any, knownFacts map[string]*providersupport.FactSpec, selfFact string) (string, bool) {
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	if s == selfFact {
		return "", false
	}
	if _, ok := knownFacts[s]; !ok {
		return "", false
	}
	return s, true
}

// bindingsConflict reports whether any key in candidate is already
// bound in existing to a different value.
func bindingsConflict(candidate, existing map[string]any) bool {
	for k, cv := range candidate {
		if ev, ok := existing[k]; ok && ev != cv {
			return true
		}
	}
	return false
}

// mergeInto copies src into dst without overwriting existing bindings.
func mergeInto(dst, src map[string]any) {
	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}
