// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"testing"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// twoOperatorModel builds a pair of components where 'web' depends on
// 'eso', and eso is type 'operator' with a healthy rule
// deployment_ready==true. Mirrors the real model's external-secrets
// situation at a miniature scale.
func twoOperatorModel(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()
	operatorType := &providersupport.Type{
		Name: "operator",
		Facts: map[string]*providersupport.FactSpec{
			"deployment_ready": {Probe: providersupport.ProbeDef{Cmd: "op-dr", Cost: "low", Access: "read"}},
		},
		Healthy: []expr.Node{
			expr.CmpNode{Fact: "deployment_ready", Op: expr.OpEq, Value: true},
		},
		States: []providersupport.StateDef{
			{Name: "not_running", When: expr.CmpNode{Fact: "deployment_ready", Op: expr.OpEq, Value: false}},
			{Name: "ready", When: expr.CmpNode{Fact: "deployment_ready", Op: expr.OpEq, Value: true}},
		},
		DefaultActiveState: "ready",
	}
	webType := &providersupport.Type{
		Name: "web",
		Facts: map[string]*providersupport.FactSpec{
			"up": {Probe: providersupport.ProbeDef{Cmd: "w-up", Cost: "low", Access: "read"}},
		},
		Healthy: []expr.Node{
			expr.CmpNode{Fact: "up", Op: expr.OpEq, Value: true},
		},
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{"operator": operatorType, "web": webType},
	})
	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"web": {Name: "web", Type: "web", Depends: []model.Dependency{{On: []string{"eso"}}}},
			"eso": {Name: "eso", Type: "operator"},
		},
		Order: []string{"web", "eso"},
	}
	m.BuildGraph()
	return m, reg
}

// After BFS has collected every reachable fact and everything is healthy,
// Done must be returned with no RootCause (preserves prior behaviour).
func TestBFS_DoneNoRootWhenAllHealthy(t *testing.T) {
	m, reg := twoOperatorModel(t)
	store := facts.NewInMemory()
	store.Append("web", facts.Fact{Key: "up", Value: true})
	store.Append("eso", facts.Fact{Key: "deployment_ready", Value: true})

	dec := BFS().SuggestProbe(Input{Model: m, Registry: reg, Store: store})
	if !dec.Done {
		t.Fatalf("want Done=true; got %+v", dec)
	}
	if dec.RootCause != nil {
		t.Errorf("want no root cause when all healthy; got %+v", dec.RootCause)
	}
}

// Regression: an unhealthy upstream with no downstream symptom must
// surface as a standalone-unhealthy root cause, not "all healthy".
// See: the external-secrets-down-with-healthy-downstream incident that
// motivated this path — mgtt-diagnose would report "none — all
// components healthy" while deployment_ready=false was right there in
// the trail.
func TestBFS_StandaloneUnhealthyUpstreamReported(t *testing.T) {
	m, reg := twoOperatorModel(t)
	store := facts.NewInMemory()
	store.Append("web", facts.Fact{Key: "up", Value: true})                // downstream healthy
	store.Append("eso", facts.Fact{Key: "deployment_ready", Value: false}) // upstream broken

	dec := BFS().SuggestProbe(Input{Model: m, Registry: reg, Store: store})
	if !dec.Done {
		t.Fatalf("want Done=true; got %+v", dec)
	}
	if dec.RootCause == nil {
		t.Fatalf("want RootCause naming eso; got nil (reason=%q)", dec.Reason)
	}
	if dec.RootCause.Root.Component != "eso" {
		t.Errorf("RootCause.Component = %q, want eso", dec.RootCause.Root.Component)
	}
	if dec.RootCause.Root.State != "not_running" {
		t.Errorf("RootCause.State = %q, want not_running (from type's state table)", dec.RootCause.Root.State)
	}
}

// Regression (engine-divergence flag 1): BFS must honour dependency
// while-guards exactly as engine.Plan does. A component reachable only
// through a definitively-inactive while-guarded edge is NOT in play, so
// even when it is unhealthy BFS must neither probe it nor blame it —
// otherwise the no-scenarios diagnose path would name a root cause that
// simulate (engine.Plan) excludes.
func TestBFS_WhileGuardInactiveExcludesComponent(t *testing.T) {
	m, reg := twoOperatorModel(t)
	// web depends on eso only WHILE web.up == false. We'll set web.up =
	// true below, making the guard definitively false → eso unreachable.
	m.Components["web"].Depends = []model.Dependency{
		{On: []string{"eso"}, While: expr.CmpNode{Fact: "up", Op: expr.OpEq, Value: false}},
	}
	m.BuildGraph()

	store := facts.NewInMemory()
	store.Append("web", facts.Fact{Key: "up", Value: true})                // guard false → eso inactive
	store.Append("eso", facts.Fact{Key: "deployment_ready", Value: false}) // eso broken, but off-path

	dec := BFS().SuggestProbe(Input{Model: m, Registry: reg, Store: store})
	if !dec.Done {
		t.Fatalf("want Done=true (web's only fact collected, eso off-path); got %+v", dec)
	}
	if dec.RootCause != nil {
		t.Errorf("want no root cause — eso is behind an inactive while-guard; got %q", dec.RootCause.Root.Component)
	}
}

// When both an upstream and its downstream are unhealthy, pick the
// upstream — it's the deepest-upstream offender, everything
// downstream is a symptom.
func TestBFS_StandaloneUnhealthyPrefersDeepestUpstream(t *testing.T) {
	m, reg := twoOperatorModel(t)
	store := facts.NewInMemory()
	store.Append("web", facts.Fact{Key: "up", Value: false})               // downstream also unhealthy
	store.Append("eso", facts.Fact{Key: "deployment_ready", Value: false}) // upstream unhealthy

	dec := BFS().SuggestProbe(Input{Model: m, Registry: reg, Store: store})
	if dec.RootCause == nil {
		t.Fatalf("want RootCause; got nil")
	}
	if dec.RootCause.Root.Component != "eso" {
		t.Errorf("want upstream eso as root; got %q", dec.RootCause.Root.Component)
	}
}

// Component-level healthy override must take precedence over the
// type's healthy rules when deciding standalone unhealth. Mirrors the
// magento-platform model's rds/redis/mq overrides.
func TestBFS_StandaloneRespectsComponentLevelHealthy(t *testing.T) {
	typ := &providersupport.Type{
		Name: "svc",
		Facts: map[string]*providersupport.FactSpec{
			"connection_count": {Probe: providersupport.ProbeDef{Cmd: "c", Cost: "low", Access: "read"}},
		},
		// Type says always healthy (no rules).
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{"svc": typ},
	})
	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			// Component override: unhealthy when connection_count >= 500.
			"db": {
				Name: "db",
				Type: "svc",
				Healthy: []expr.Node{
					expr.CmpNode{Fact: "connection_count", Op: expr.OpLt, Value: float64(500)},
				},
			},
		},
		Order: []string{"db"},
	}
	m.BuildGraph()
	store := facts.NewInMemory()
	store.Append("db", facts.Fact{Key: "connection_count", Value: 700})

	dec := BFS().SuggestProbe(Input{Model: m, Registry: reg, Store: store})
	if dec.RootCause == nil {
		t.Fatalf("want RootCause from component-level override; got nil")
	}
	if dec.RootCause.Root.Component != "db" {
		t.Errorf("want db flagged; got %q", dec.RootCause.Root.Component)
	}
}
