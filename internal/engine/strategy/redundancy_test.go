// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// groupModel: edge needs one of web-a, web-b; both read store.
func groupModel(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()
	_, reg := layeredModel(t)
	path := filepath.Join(t.TempDir(), "system.model.yaml")
	body := `meta:
  name: group
  version: "1.0"
  providers: [compute, datalayer]
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
	return m, reg
}

func reachesEdge(scs []scenarios.Scenario, root string) bool {
	for _, s := range scs {
		if s.Root.Component == root && s.TouchesComponent("edge") {
			return true
		}
	}
	return false
}

// One member failing leaves need: 1 of 2 holding, so no chain carries it
// to edge; a store under both members takes the group down, so its
// chains do.
func TestEnumerate_GroupStopsSingleMemberNotCommonCause(t *testing.T) {
	m, reg := groupModel(t)
	scs := scenarios.Enumerate(m, reg)
	if reachesEdge(scs, "web-a") || reachesEdge(scs, "web-b") {
		t.Error("a single web failing must not reach edge through need: 1 of 2")
	}
	if !reachesEdge(scs, "store") {
		t.Error("store fails both webs: its chains must reach edge")
	}
}

// At diagnosis time a group that facts show still holding contradicts
// any chain passing through it, even a common-cause one.
func TestFilterLive_SatisfiedGroupKillsChainThroughIt(t *testing.T) {
	m, reg := groupModel(t)
	store := facts.NewInMemory()
	for k, v := range map[string]any{"ready_replicas": 2, "desired_replicas": 2, "restart_count": 0, "endpoints": 2} {
		store.Append("web-b", facts.Fact{Key: k, Value: v, At: time.Now()})
	}
	chain := scenarios.Scenario{ID: "s", Chain: []scenarios.Step{
		{Component: "store", State: "stopped"},
		{Component: "web-a", State: "degraded"},
		{Component: "edge", State: "draining", Observes: []string{"upstream_count"}},
	}}
	if live := FilterLive([]scenarios.Scenario{chain}, store, m, reg); len(live) != 0 {
		t.Fatal("web-b healthy keeps edge's group satisfied: a chain through web-a to edge is contradicted")
	}
}
