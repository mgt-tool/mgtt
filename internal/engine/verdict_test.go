// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/facts"
)

// Unknown is never healthy: a component whose every probe came back
// forbidden or transient has not been observed at all, so it must not be
// eliminated — least of all with a "healthy" reason. Before the fix the
// path engine cleared `store` and blamed its casualty `api`.
func TestPlan_UnresolvedFactsNeverEliminate(t *testing.T) {
	for _, status := range []facts.FactStatus{facts.FactStatusForbidden, facts.FactStatusTransient} {
		t.Run(string(status), func(t *testing.T) {
			m, reg := loadStorefront(t)
			store := newStore(map[string]map[string]any{
				"api": {"ready_replicas": 0, "restart_count": 12, "desired_replicas": 3},
			})
			for _, k := range []string{"available", "connection_count"} {
				store.Append("store", facts.Fact{Key: k, Status: status, At: time.Now()})
			}

			tree := Plan(m, reg, store, "")

			for _, c := range eliminatedComponents(tree) {
				if c == "store" {
					t.Fatalf("store (every fact %s) was eliminated", status)
				}
			}
			if len(tree.CannotRuleOut) != 1 || tree.CannotRuleOut[0].Component != "store" {
				t.Fatalf("conclusion must name store as not ruled out; got %+v", tree.CannotRuleOut)
			}
		})
	}
}

// An alive path whose tail nobody has probed keeps the engine probing
// inward, but it is not a verdict: with api crash-looping and store not
// yet looked at, the root cause is api, not the deeper store.
func TestPlan_UnprobedTailIsNotTheRootCause(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"api": {"ready_replicas": 0, "restart_count": 12, "desired_replicas": 3, "endpoints": 0},
	})

	tree := Plan(m, reg, store, "")

	if tree.RootCause != "api" {
		t.Errorf("root_cause = %q, want api", tree.RootCause)
	}
	if !sliceContains(aliveTails(tree), "store") {
		t.Errorf("store path should stay alive to be probed next; alive tails %v", aliveTails(tree))
	}
}

// A failure reaches the entry only through every link of its path, so an
// unprobed tail behind a component proven healthy is refuted. A tail that
// is itself seen broken stays a finding either way.
func TestPlan_HealthyLinkRefutesUnobservedTail(t *testing.T) {
	m, reg := loadStorefront(t)
	healthyAPI := map[string]any{"ready_replicas": 3, "restart_count": 0, "desired_replicas": 3, "endpoints": 3}
	// edge is sick above the healthy api, the storefront's shape: before,
	// any sick ancestor kept the unprobed store alive and, being deepest,
	// blamed.
	sickEdge := map[string]any{"upstream_count": 0}

	tree := Plan(m, reg, newStore(map[string]map[string]any{"api": healthyAPI, "edge": sickEdge}), "")
	if sliceContains(aliveTails(tree), "store") {
		t.Errorf("unprobed store behind healthy api should be eliminated; alive tails %v", aliveTails(tree))
	}

	tree = Plan(m, reg, newStore(map[string]map[string]any{
		"api": healthyAPI, "store": {"available": false, "connection_count": 0},
	}), "")
	if tree.RootCause != "store" {
		t.Errorf("a store seen down is the root cause even behind a healthy api; got %q", tree.RootCause)
	}
}

func aliveTails(tree *PathTree) []string {
	var out []string
	for _, p := range tree.Paths {
		out = append(out, p.Components[len(p.Components)-1])
	}
	return out
}

func sliceContains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// A component the model expects and the probes cannot find is the
// finding: with api crash-looping and every store probe not_found, the
// deleted store is the root cause, not api.
func TestPlan_AbsentComponentIsTheRootCause(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"api": {"ready_replicas": 0, "restart_count": 12, "desired_replicas": 3, "endpoints": 0},
	})
	for _, k := range []string{"available", "connection_count"} {
		store.Append("store", facts.Fact{Key: k, Status: facts.FactStatusNotFound, At: time.Now()})
	}

	tree := Plan(m, reg, store, "")

	if tree.RootCause != "store" {
		t.Errorf("root_cause = %q, want store (not found)", tree.RootCause)
	}
}
