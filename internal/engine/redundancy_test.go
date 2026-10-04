// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
)

// redundantModel: edge serves traffic through web-a OR web-b (need: 1);
// both read store.
func redundantModel(t *testing.T) *model.Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "system.model.yaml")
	body := `meta:
  name: redundant
  version: "1.0"
  providers: [compute]
components:
  edge:
    type: gateway
    depends:
      - on: [web-a, web-b]
        need: 1
  web-a:
    type: workload
    depends:
      - on: store
  web-b:
    type: workload
    depends:
      - on: store
  store:
    providers: [datalayer]
    type: datastore
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

var (
	webUp    = map[string]any{"ready_replicas": 2, "desired_replicas": 2, "restart_count": 0, "endpoints": 2}
	webDown  = map[string]any{"ready_replicas": 0, "desired_replicas": 2, "restart_count": 9, "endpoints": 0}
	edgeUp   = map[string]any{"upstream_count": 2}
	edgeDown = map[string]any{"upstream_count": 0}
	storeUp  = map[string]any{"available": true, "connection_count": 10}
)

// With need: 1, one healthy web keeps edge served: the broken one is
// degraded redundancy, reported as such, not the root cause -- and both
// engines say so.
func TestRedundancy_GroupDecidesTheRootCause(t *testing.T) {
	m := redundantModel(t)
	_, reg := loadStorefront(t)
	for _, tc := range []struct {
		name         string
		facts        map[string]map[string]any
		wantRoot     string
		wantDegraded int
	}{
		{"one web down, edge fine", map[string]map[string]any{"edge": edgeUp, "web-a": webDown, "web-b": webUp, "store": storeUp}, "", 1},
		{"one web down, edge broken anyway", map[string]map[string]any{"edge": edgeDown, "web-a": webDown, "web-b": webUp, "store": storeUp}, "edge", 1},
		{"both webs down", map[string]map[string]any{"edge": edgeDown, "web-a": webDown, "web-b": webDown, "store": storeUp}, "web-a", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore(tc.facts)
			tree := Plan(m, reg, store, "")
			if tree.RootCause != tc.wantRoot {
				t.Errorf("plan root = %q, want %q", tree.RootCause, tc.wantRoot)
			}
			if len(tree.RedundancyDegraded) != tc.wantDegraded {
				t.Errorf("degraded = %v, want %d", tree.RedundancyDegraded, tc.wantDegraded)
			}
			if got := bfsRootComponent(t, m, reg, store); got != tc.wantRoot {
				t.Errorf("diagnose (bfs) root = %q, want %q: the engines disagree", got, tc.wantRoot)
			}
		})
	}
}

// A member whose health is unknown does not count toward need: an unread
// fact must not let a group look satisfied and hide the failure.
func TestRedundancy_UnknownMemberDoesNotSatisfy(t *testing.T) {
	m := redundantModel(t)
	_, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{"edge": edgeDown, "web-a": webDown, "store": storeUp})
	for _, k := range []string{"ready_replicas", "restart_count"} {
		store.Append("web-b", facts.Fact{Key: k, Status: facts.FactStatusForbidden, At: time.Now()})
	}
	if tree := Plan(m, reg, store, ""); tree.RootCause != "web-a" {
		t.Errorf("root = %q, want web-a: an unknown web-b cannot cover for it", tree.RootCause)
	}
}
