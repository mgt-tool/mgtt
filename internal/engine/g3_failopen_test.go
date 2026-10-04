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
		})
	}
}
