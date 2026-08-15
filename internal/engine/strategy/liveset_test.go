// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

func TestFilterLive_NoFacts_AllLive(t *testing.T) {
	scs := []scenarios.Scenario{
		{ID: "s-1", Chain: []scenarios.Step{{Component: "rds", State: "stopped"}}},
		{ID: "s-2", Chain: []scenarios.Step{{Component: "api", State: "crashed"}}},
	}
	store := facts.NewInMemory()
	live := FilterLive(scs, store, nil, nil)
	if len(live) != 2 {
		t.Errorf("no facts → all live; got %d", len(live))
	}
}

// TestFilterLive_ContradictedStateEliminated builds a minimal type with a
// `stopped` state whose `when` predicate is `status == "stopped"`. A fact
// (status="running") contradicts it, so scenarios referencing that state
// should be filtered out.
func TestFilterLive_ContradictedStateEliminated(t *testing.T) {
	// Build a minimal type that has one state 'stopped' with a
	// contradicted predicate.
	whenStopped := expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "stopped"}
	typ := &providersupport.Type{
		Name: "service",
		States: []providersupport.StateDef{
			{Name: "stopped", When: whenStopped},
		},
	}
	prov := &providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{"service": typ},
	}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"svc": {Name: "svc", Type: "service"},
		},
		Order: []string{"svc"},
	}

	store := facts.NewInMemory()
	store.Append("svc", facts.Fact{Key: "status", Value: "running", At: time.Now()})

	scs := []scenarios.Scenario{
		{ID: "contradicted", Chain: []scenarios.Step{{Component: "svc", State: "stopped"}}},
		{ID: "unrelated", Chain: []scenarios.Step{{Component: "other", State: "down"}}},
	}
	live := FilterLive(scs, store, m, reg)
	if len(live) != 1 {
		t.Fatalf("want 1 live (unrelated); got %d: %+v", len(live), live)
	}
	if live[0].ID != "unrelated" {
		t.Errorf("wrong scenario survived: %s", live[0].ID)
	}
}

// TestFilterLive_ConfirmedStateKept verifies that a step whose state
// predicate evaluates true (i.e., the state is confirmed by facts) keeps
// the scenario alive.
func TestFilterLive_ConfirmedStateKept(t *testing.T) {
	whenStopped := expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "stopped"}
	typ := &providersupport.Type{
		Name: "service",
		States: []providersupport.StateDef{
			{Name: "stopped", When: whenStopped},
		},
	}
	prov := &providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{"service": typ},
	}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"svc": {Name: "svc", Type: "service"},
		},
		Order: []string{"svc"},
	}

	store := facts.NewInMemory()
	store.Append("svc", facts.Fact{Key: "status", Value: "stopped", At: time.Now()})

	scs := []scenarios.Scenario{
		{ID: "confirmed", Chain: []scenarios.Step{{Component: "svc", State: "stopped"}}},
	}
	live := FilterLive(scs, store, m, reg)
	if len(live) != 1 {
		t.Fatalf("confirmed scenario should be live; got %d", len(live))
	}
}

// TestStepConsistent_UnresolvedErrorKeepsLive — a step whose when-predicate
// references a fact not in the store should be kept alive (UnresolvedError
// is not a genuine evaluator failure).
func TestStepConsistent_UnresolvedErrorKeepsLive(t *testing.T) {
	whenStopped := expr.CmpNode{Fact: "missing_fact", Op: expr.OpEq, Value: "x"}
	typ := &providersupport.Type{
		Name: "service",
		States: []providersupport.StateDef{
			{Name: "stopped", When: whenStopped},
		},
	}
	prov := &providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{"service": typ},
	}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"svc": {Name: "svc", Type: "service"},
		},
		Order: []string{"svc"},
	}

	store := facts.NewInMemory()
	store.Append("svc", facts.Fact{Key: "other", Value: "x", At: time.Now()})

	step := scenarios.Step{Component: "svc", State: "stopped"}
	if !stepConsistent(step, store, m, reg) {
		t.Error("UnresolvedError must keep step alive")
	}
}

// TestStepConsistent_GenuineEvalErrorMarksContradicted — an evaluator error
// that is NOT an UnresolvedError (e.g., an operator the type-compare can't
// support) must eliminate the step, not silently keep it alive.
func TestStepConsistent_GenuineEvalErrorMarksContradicted(t *testing.T) {
	// Predicate: available < false — OpLt on a bool raises a genuine
	// (non-UnresolvedError) error in compareBools.
	whenStopped := expr.CmpNode{Fact: "available", Op: expr.OpLt, Value: false}
	typ := &providersupport.Type{
		Name: "service",
		States: []providersupport.StateDef{
			{Name: "stopped", When: whenStopped},
		},
	}
	prov := &providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{"service": typ},
	}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"svc": {Name: "svc", Type: "service"},
		},
		Order: []string{"svc"},
	}

	store := facts.NewInMemory()
	// Bool fact so the compare path is bool-on-bool with an invalid op.
	store.Append("svc", facts.Fact{Key: "available", Value: true, At: time.Now()})

	step := scenarios.Step{Component: "svc", State: "stopped"}
	if stepConsistent(step, store, m, reg) {
		t.Error("genuine evaluator error must eliminate the step, not keep it alive")
	}
}

// TestFilterLive_UndefinedPredicateKeeps verifies that missing facts
// (UnresolvedError) keep the scenario live rather than eliminating it.
func TestFilterLive_UndefinedPredicateKeeps(t *testing.T) {
	// Predicate references a fact we never recorded.
	whenStopped := expr.CmpNode{Fact: "missing_fact", Op: expr.OpEq, Value: "x"}
	typ := &providersupport.Type{
		Name: "service",
		States: []providersupport.StateDef{
			{Name: "stopped", When: whenStopped},
		},
	}
	prov := &providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{"service": typ},
	}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"svc": {Name: "svc", Type: "service"},
		},
		Order: []string{"svc"},
	}

	store := facts.NewInMemory()
	// Record some unrelated fact so FactsFor returns non-nil.
	store.Append("svc", facts.Fact{Key: "other", Value: "x", At: time.Now()})

	scs := []scenarios.Scenario{
		{ID: "undefined", Chain: []scenarios.Step{{Component: "svc", State: "stopped"}}},
	}
	live := FilterLive(scs, store, m, reg)
	if len(live) != 1 {
		t.Fatalf("undefined predicate should keep scenario live; got %d", len(live))
	}
}

// TestFilterLive_AbsentComponentEliminatesFailureStates verifies that
// once the probe layer has recorded a not_found fact, scenarios that
// require a non-default state on that component are eliminated — a
// missing component can't be "stopped", "draining", etc.
func TestFilterLive_AbsentComponentEliminatesFailureStates(t *testing.T) {
	typ := &providersupport.Type{
		Name:               "service",
		DefaultActiveState: "live",
		States: []providersupport.StateDef{
			{Name: "stopped", When: expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "stopped"}},
			{Name: "live"},
		},
	}
	prov := &providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{"service": typ},
	}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"svc": {Name: "svc", Type: "service"},
		},
		Order: []string{"svc"},
	}

	store := facts.NewInMemory()
	store.Append("svc", facts.Fact{Key: "exists", Status: facts.FactStatusNotFound, At: time.Now()})

	scs := []scenarios.Scenario{
		{ID: "stopped", Chain: []scenarios.Step{{Component: "svc", State: "stopped"}}},
		{ID: "live", Chain: []scenarios.Step{{Component: "svc", State: "live"}}},
	}
	live := FilterLive(scs, store, m, reg)
	if len(live) != 1 {
		t.Fatalf("want 1 live (default-state); got %d: %+v", len(live), live)
	}
	if live[0].ID != "live" {
		t.Errorf("absent component: only the default-active state should stay live; got %s", live[0].ID)
	}
}

// TestFilterLive_AbsentComponentWithoutDefaultStateEliminatesAll — when
// the type declares no default_active_state, an absent component has
// no "harmless" survivor state and every scenario referencing it dies.
func TestFilterLive_AbsentComponentWithoutDefaultStateEliminatesAll(t *testing.T) {
	typ := &providersupport.Type{
		Name: "service",
		States: []providersupport.StateDef{
			{Name: "any", When: expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "x"}},
		},
	}
	prov := &providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{"service": typ},
	}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"svc": {Name: "svc", Type: "service"},
		},
		Order: []string{"svc"},
	}

	store := facts.NewInMemory()
	store.Append("svc", facts.Fact{Key: "exists", Status: facts.FactStatusNotFound, At: time.Now()})

	scs := []scenarios.Scenario{
		{ID: "any", Chain: []scenarios.Step{{Component: "svc", State: "any"}}},
	}
	live := FilterLive(scs, store, m, reg)
	if len(live) != 0 {
		t.Fatalf("type with no default_active_state: absent component eliminates every scenario; got %d live", len(live))
	}
}
