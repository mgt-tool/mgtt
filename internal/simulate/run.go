// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"sort"
	"time"

	"github.com/mgt-tool/mgtt/internal/engine"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// Run executes a single simulation scenario against the model and returns
// the result, including whether the scenario's expectations were met.
func Run(m *model.Model, reg *providersupport.Registry, sc *Scenario) *Result {
	store := facts.NewInMemory()
	for comp, kvs := range sc.Inject {
		for k, v := range kvs {
			store.Append(comp, facts.Fact{
				Key:       k,
				Value:     v,
				Collector: "simulate",
				At:        time.Now(),
			})
		}
	}

	tree := engine.Plan(m, reg, store, "")
	actual := extractConclusion(tree)
	pass := matches(sc.Expect, actual)

	return &Result{
		Scenario: sc,
		Actual:   actual,
		Pass:     pass,
	}
}

// extractConclusion derives the actual Expectation from a PathTree.
func extractConclusion(tree *engine.PathTree) Expectation {
	var ex Expectation

	// Root cause
	ex.RootCause = tree.RootCause
	if ex.RootCause == "" {
		ex.RootCause = "none"
	}

	// Path: find the surviving path whose terminal component is the root cause.
	if tree.RootCause != "" {
		for _, p := range tree.Paths {
			last := p.Components[len(p.Components)-1]
			if last == tree.RootCause {
				ex.Path = p.Components
				break
			}
		}
	}

	elim := engine.EliminatedOnly(tree)
	sort.Strings(elim)
	ex.Eliminated = elim
	return ex
}

// matches compares expected vs actual expectations.
//
// Semantics by field:
//
//	RootCause   strict equality (required)
//	Path        ordered subsequence — every element of expected.Path
//	            must appear in actual.Path in the given order; extras
//	            between them are allowed. A scenario that asserts
//	            [nginx, api, rds] still passes if actual is
//	            [nginx, api, legacy-gateway, rds] because the model
//	            grew a middle hop. Omitted = not asserted.
//	Eliminated  subset — every element of expected.Eliminated must be
//	            in actual.Eliminated; actual may contain more. Adding
//	            a topology-only component to the model doesn't
//	            cascade-break every scenario's eliminated set.
//
// This matcher used to be strict equality on both Path and Eliminated,
// which made scenarios brittle to any topology change — a new
// component, a split service, an extra intermediate hop from a
// catalog source — whether the logical root cause changed or not. The
// relaxation asserts that the expected shape is PRESENT in the actual
// result, not that the actual result contains NOTHING else. Every
// scenario that passed under the strict matcher still passes here.
func matches(expected, actual Expectation) bool {
	if expected.RootCause != actual.RootCause {
		return false
	}
	if !isOrderedSubsequence(expected.Path, actual.Path) {
		return false
	}
	if !isSubset(expected.Eliminated, actual.Eliminated) {
		return false
	}
	return true
}

// isOrderedSubsequence reports whether every element of sub appears in
// seq in the given order (not necessarily contiguously). Empty sub is
// a subsequence of any seq ("no assertion" semantics).
func isOrderedSubsequence(sub, seq []string) bool {
	if len(sub) == 0 {
		return true
	}
	i := 0
	for _, s := range seq {
		if s == sub[i] {
			i++
			if i == len(sub) {
				return true
			}
		}
	}
	return false
}

// isSubset reports whether every element of sub is present in super.
// Empty sub is a subset of any super ("no assertion" semantics).
func isSubset(sub, super []string) bool {
	if len(sub) == 0 {
		return true
	}
	have := make(map[string]struct{}, len(super))
	for _, s := range super {
		have[s] = struct{}{}
	}
	for _, s := range sub {
		if _, ok := have[s]; !ok {
			return false
		}
	}
	return true
}
