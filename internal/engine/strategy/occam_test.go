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

// threeCompModel builds a three-component model (web → api → db) all
// backed by the same provider "p" with a single-state single-fact type
// per component. Used as the fixture for occam tests.
func threeCompModel(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()
	mk := func(name string) *providersupport.Type {
		return &providersupport.Type{
			Name: name,
			Facts: map[string]*providersupport.FactSpec{
				"status": {Probe: providersupport.ProbeDef{Cmd: name + "-status", Cost: "cheap", Access: "read"}},
			},
			States: []providersupport.StateDef{
				{Name: "down", When: expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "down"}},
			},
		}
	}
	prov := &providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{
			"web": mk("web"),
			"api": mk("api"),
			"db":  mk("db"),
		},
	}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"web": {Name: "web", Type: "web", Depends: []model.Dependency{{On: []string{"api"}}}},
			"api": {Name: "api", Type: "api", Depends: []model.Dependency{{On: []string{"db"}}}},
			"db":  {Name: "db", Type: "db"},
		},
		Order: []string{"web", "api", "db"},
	}
	m.BuildGraph()
	return m, reg
}

func TestOccam_Name(t *testing.T) {
	if Occam().Name() != "occam" {
		t.Errorf("want occam; got %s", Occam().Name())
	}
}

func TestOccam_PicksShortestScenarioFirst(t *testing.T) {
	m, reg := threeCompModel(t)
	store := facts.NewInMemory()

	scs := []scenarios.Scenario{
		{
			ID: "long",
			Chain: []scenarios.Step{
				{Component: "web", State: "down"},
				{Component: "api", State: "down"},
				{Component: "db", State: "down"},
			},
		},
		{
			ID:    "short",
			Chain: []scenarios.Step{{Component: "web", State: "down"}},
		},
	}

	dec := Occam().SuggestProbe(Input{Model: m, Registry: reg, Store: store, Scenarios: scs})
	if dec.Probe == nil {
		t.Fatalf("want probe; got %+v", dec)
	}
	// Shortest is "short" → its only step is web → probe targets web.
	if dec.Probe.Component != "web" {
		t.Errorf("want probe on web (shortest scenario's terminal); got %q", dec.Probe.Component)
	}
}

func TestOccam_StuckWhenAllContradicted(t *testing.T) {
	m, reg := threeCompModel(t)
	store := facts.NewInMemory()
	// Record a fact that contradicts 'web.down' (status=up, not down).
	store.Append("web", facts.Fact{Key: "status", Value: "up", At: time.Now()})

	scs := []scenarios.Scenario{
		{ID: "a", Chain: []scenarios.Step{{Component: "web", State: "down"}}},
		{ID: "b", Chain: []scenarios.Step{
			{Component: "web", State: "down"},
			{Component: "api", State: "down"},
		}},
	}

	dec := Occam().SuggestProbe(Input{Model: m, Registry: reg, Store: store, Scenarios: scs})
	if !dec.Stuck {
		t.Fatalf("want Stuck=true; got %+v", dec)
	}
}

func TestOccam_DoneWhenSingleRemains(t *testing.T) {
	m, reg := threeCompModel(t)
	store := facts.NewInMemory()
	scs := []scenarios.Scenario{
		{ID: "only", Chain: []scenarios.Step{{Component: "web", State: "down"}}},
	}

	dec := Occam().SuggestProbe(Input{Model: m, Registry: reg, Store: store, Scenarios: scs})
	if !dec.Done {
		t.Fatalf("want Done=true; got %+v", dec)
	}
	if dec.RootCause == nil || dec.RootCause.ID != "only" {
		t.Errorf("want RootCause=only; got %+v", dec.RootCause)
	}
}

// TestOccam_PicksUnverifiedFactAtFactLevel verifies the M3 fix: a
// component-level "any fact present → skip" gate would wrongly treat a
// step as verified when an *unrelated* fact is collected. The fact-level
// gate must still probe the specific fact(s) the step depends on.
func TestOccam_PicksUnverifiedFactAtFactLevel(t *testing.T) {
	// Type 'rds' has two facts: "available" (referenced by state.stopped's
	// when) and "other_fact" (unrelated). State 'stopped' is non-terminal
	// for the test scenario — we'll put it mid-chain so the observes list
	// is empty and stepObservingFacts must walk the When predicate.
	rdsType := &providersupport.Type{
		Name: "rds",
		Facts: map[string]*providersupport.FactSpec{
			"available":  {Probe: providersupport.ProbeDef{Cmd: "rds-avail", Cost: "cheap", Access: "read"}},
			"other_fact": {Probe: providersupport.ProbeDef{Cmd: "rds-other", Cost: "cheap", Access: "read"}},
		},
		States: []providersupport.StateDef{
			{Name: "stopped", When: expr.CmpNode{Fact: "available", Op: expr.OpEq, Value: false}},
		},
	}
	apiType := &providersupport.Type{
		Name: "api",
		Facts: map[string]*providersupport.FactSpec{
			"error_rate": {Probe: providersupport.ProbeDef{Cmd: "api-err", Cost: "cheap", Access: "read"}},
		},
		States: []providersupport.StateDef{
			{Name: "down"},
		},
	}
	prov := &providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{
			"rds": rdsType,
			"api": apiType,
		},
	}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"rds": {Name: "rds", Type: "rds"},
			"api": {Name: "api", Type: "api", Depends: []model.Dependency{{On: []string{"rds"}}}},
		},
		Order: []string{"rds", "api"},
	}

	store := facts.NewInMemory()
	// Collect an UNRELATED fact on rds. Component-level gate would skip
	// rds as "has facts". Fact-level gate must still probe "available".
	store.Append("rds", facts.Fact{Key: "other_fact", Value: "x", At: time.Now()})

	// Pre-collect error_rate on api too — this makes api.down's terminal
	// step fully verified (all observed facts present), forcing the
	// symptom-inward walk to continue to rds.stopped.
	store.Append("api", facts.Fact{Key: "error_rate", Value: 0.0, At: time.Now()})

	// Two scenarios so Occam doesn't short-circuit as Done. Both share
	// the rds→api shape; pick either — the walk on either hits rds.
	scs := []scenarios.Scenario{
		{
			ID: "rds-first",
			Chain: []scenarios.Step{
				{Component: "rds", State: "stopped", EmitsOnEdge: "sig"},
				{Component: "api", State: "down", Observes: []string{"error_rate"}},
			},
		},
		{
			ID: "rds-alt",
			Chain: []scenarios.Step{
				{Component: "rds", State: "stopped", EmitsOnEdge: "sig2"},
				{Component: "api", State: "down", Observes: []string{"error_rate"}},
			},
		},
	}

	dec := Occam().SuggestProbe(Input{Model: m, Registry: reg, Store: store, Scenarios: scs})
	if dec.Probe == nil {
		t.Fatalf("want probe; got %+v", dec)
	}
	// Fact-level gate: rds has other_fact collected, but "available"
	// (the fact referenced by state.stopped's when) is missing. Probe
	// must target rds.available despite other_fact being present.
	if dec.Probe.Component != "rds" || dec.Probe.Fact != "available" {
		t.Errorf("want probe on rds.available (fact-level gate); got %s.%s",
			dec.Probe.Component, dec.Probe.Fact)
	}
}

func TestOccam_SuspectBumpsTieBreak(t *testing.T) {
	m, reg := threeCompModel(t)
	store := facts.NewInMemory()
	// Two scenarios of equal length (2).
	scs := []scenarios.Scenario{
		{
			ID: "ignored",
			Chain: []scenarios.Step{
				{Component: "web", State: "down"},
				{Component: "api", State: "down"},
			},
		},
		{
			ID: "suspected",
			Chain: []scenarios.Step{
				{Component: "web", State: "down"},
				{Component: "db", State: "down"},
			},
		},
	}
	hints := []SuspectHint{{Component: "db"}}

	dec := Occam().SuggestProbe(Input{
		Model: m, Registry: reg, Store: store,
		Scenarios: scs, Suspects: hints,
	})
	if dec.Probe == nil {
		t.Fatalf("want probe; got %+v", dec)
	}
	// Suspected scenario's terminal is db → probe should target db.
	if dec.Probe.Component != "db" {
		t.Errorf("want probe on db (suspect-touching scenario's terminal); got %q", dec.Probe.Component)
	}
}
