// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package engine

import (
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func loadStorefront(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()
	root := repoRoot()
	m, err := model.Load(filepath.Join(root, "testdata", "models", "sample-model.yaml"))
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	reg := providersupport.NewRegistry()
	for _, pair := range []struct{ name, file string }{
		{"compute", "../../testdata/providers/compute.yaml"},
		{"datalayer", "../../testdata/providers/datalayer.yaml"},
	} {
		p, err := providersupport.LoadFromFile(pair.file)
		if err != nil {
			t.Fatalf("load provider %s: %v", pair.name, err)
		}
		reg.Register(p)
	}
	return m, reg
}

func newStore(data map[string]map[string]any) *facts.Store {
	store := facts.NewInMemory()
	for comp, kv := range data {
		for k, v := range kv {
			store.Append(comp, facts.Fact{Key: k, Value: v, At: time.Now()})
		}
	}
	return store
}

func eliminatedComponents(tree *PathTree) []string {
	surviving := map[string]bool{}
	for _, p := range tree.Paths {
		for _, c := range p.Components {
			surviving[c] = true
		}
	}
	seen := map[string]bool{}
	var result []string
	for _, p := range tree.Eliminated {
		for _, c := range p.Components {
			if !surviving[c] && !seen[c] {
				seen[c] = true
				result = append(result, c)
			}
		}
	}
	sort.Strings(result)
	return result
}

func rootCausePath(tree *PathTree) []string {
	if tree.RootCause == "" {
		return nil
	}
	for _, p := range tree.Paths {
		last := p.Components[len(p.Components)-1]
		if last == tree.RootCause {
			return p.Components
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestPlan_EntryPoint(t *testing.T) {
	m, reg := loadStorefront(t)
	store := facts.NewInMemory()
	tree := Plan(m, reg, store, "")
	if tree.Entry != "edge" {
		t.Errorf("entry = %q, want %q", tree.Entry, "edge")
	}
}

func TestPlan_ExplicitEntry(t *testing.T) {
	m, reg := loadStorefront(t)
	store := facts.NewInMemory()
	tree := Plan(m, reg, store, "api")
	if tree.Entry != "api" {
		t.Errorf("entry = %q, want %q", tree.Entry, "api")
	}
}

// Scenario 1: store unavailable — store stops, api crash-loops as a result.
// Engine should trace fault to store, not api.
func TestPlan_RDSUnavailable(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"store": {"available": false, "connection_count": 0},
		"api":   {"ready_replicas": 0, "restart_count": 12, "desired_replicas": 3},
	})

	tree := Plan(m, reg, store, "")

	if tree.RootCause != "store" {
		t.Errorf("root_cause = %q, want %q", tree.RootCause, "store")
	}

	path := rootCausePath(tree)
	wantPath := []string{"edge", "api", "store"}
	if !sliceEqual(path, wantPath) {
		t.Errorf("root cause path = %v, want %v", path, wantPath)
	}

	elim := eliminatedComponents(tree)
	wantElim := []string{"frontend"}
	if !sliceEqual(elim, wantElim) {
		t.Errorf("eliminated = %v, want %v", elim, wantElim)
	}
}

// Scenario 2: api crash-loop independent of store — store is healthy.
func TestPlan_APICrashLoop(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"api":   {"ready_replicas": 0, "restart_count": 24, "desired_replicas": 3},
		"store": {"available": true, "connection_count": 120},
	})

	tree := Plan(m, reg, store, "")

	if tree.RootCause != "api" {
		t.Errorf("root_cause = %q, want %q", tree.RootCause, "api")
	}

	path := rootCausePath(tree)
	wantPath := []string{"edge", "api"}
	if !sliceEqual(path, wantPath) {
		t.Errorf("root cause path = %v, want %v", path, wantPath)
	}

	elim := eliminatedComponents(tree)
	wantElim := []string{"frontend", "store"}
	if !sliceEqual(elim, wantElim) {
		t.Errorf("eliminated = %v, want %v", elim, wantElim)
	}
}

// Scenario 3: frontend crash-looping, api and store healthy.
func TestPlan_FrontendDegraded(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"frontend": {"ready_replicas": 0, "restart_count": 8, "desired_replicas": 2},
		"api":      {"ready_replicas": 3, "desired_replicas": 3, "endpoints": 3},
		"store":    {"available": true, "connection_count": 98},
	})

	tree := Plan(m, reg, store, "")

	if tree.RootCause != "frontend" {
		t.Errorf("root_cause = %q, want %q", tree.RootCause, "frontend")
	}

	path := rootCausePath(tree)
	wantPath := []string{"edge", "frontend"}
	if !sliceEqual(path, wantPath) {
		t.Errorf("root cause path = %v, want %v", path, wantPath)
	}

	elim := eliminatedComponents(tree)
	wantElim := []string{"api", "store"}
	if !sliceEqual(elim, wantElim) {
		t.Errorf("eliminated = %v, want %v", elim, wantElim)
	}
}

// Scenario 4: all components healthy — no false positives.
func TestPlan_AllHealthy(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"edge":     {"upstream_count": 4},
		"frontend": {"ready_replicas": 2, "desired_replicas": 2, "endpoints": 2},
		"api":      {"ready_replicas": 3, "desired_replicas": 3, "endpoints": 3},
		"store":    {"available": true, "connection_count": 87},
	})

	tree := Plan(m, reg, store, "")

	if tree.RootCause != "" {
		t.Errorf("root_cause = %q, want empty", tree.RootCause)
	}

	if len(tree.Paths) != 0 {
		t.Errorf("surviving paths = %d, want 0", len(tree.Paths))
	}

	elim := eliminatedComponents(tree)
	wantElim := []string{"api", "edge", "frontend", "store"}
	if !sliceEqual(elim, wantElim) {
		t.Errorf("eliminated = %v, want %v", elim, wantElim)
	}
}

// TestPickRootCause_PreservesDeclarationOrder guards against a regression
// where pickRootCause sorted its argument in place. Because that slice
// shares its backing array with PathTree.Paths (already sorted into
// declaration order), an in-place sort silently reordered Paths by
// descending component count, breaking deterministic output.
func TestPickRootCause_PreservesDeclarationOrder(t *testing.T) {
	alive := []Path{
		{ID: "PATH A", Components: []string{"edge"}},
		{ID: "PATH B", Components: []string{"edge", "api", "store"}},
		{ID: "PATH C", Components: []string{"edge", "frontend"}},
	}

	if rc := pickRootCause(alive); rc != "store" {
		t.Errorf("root cause = %q, want %q", rc, "store")
	}

	want := []string{"PATH A", "PATH B", "PATH C"}
	for i, p := range alive {
		if p.ID != want[i] {
			t.Errorf("alive[%d].ID = %q, want %q (pickRootCause reordered Paths)", i, p.ID, want[i])
		}
	}
}

// healthyStore returns a fully-populated store where every component
// reports healthy facts. overrides replace facts on named components to
// inject a fault. A complete store lets the BFS strategy reach a terminal
// verdict (nothing left uncollected) so it is comparable to engine.Plan.
func healthyStore(overrides map[string]map[string]any) *facts.Store {
	data := map[string]map[string]any{
		"edge":     {"upstream_count": 4},
		"frontend": {"ready_replicas": 2, "desired_replicas": 2, "restart_count": 0, "endpoints": 2},
		"api":      {"ready_replicas": 3, "desired_replicas": 3, "restart_count": 0, "endpoints": 3},
		"store":    {"available": true, "connection_count": 87},
	}
	for comp, kv := range overrides {
		data[comp] = kv
	}
	return newStore(data)
}

// bfsRootComponent drives the live-diagnose strategy terminal (BFS, the
// no-scenarios path) to completion against a complete store and returns
// the named root-cause component (empty = none). Mirrors what
// diagnoseLoop.step reports via decision.RootCause.
func bfsRootComponent(t *testing.T, m *model.Model, reg *providersupport.Registry, store *facts.Store) string {
	t.Helper()
	d := strategy.BFS().SuggestProbe(strategy.Input{Model: m, Registry: reg, Store: store})
	if !d.Done {
		t.Fatalf("BFS did not reach a verdict (store incomplete?): probe=%+v reason=%q", d.Probe, d.Reason)
	}
	if d.RootCause == nil {
		return ""
	}
	return d.RootCause.Root.Component
}

// TestEngineParity_SimulateVsDiagnose is the cross-check the
// "one engine, two contexts" promise depends on: for the same complete
// fact set, simulate (engine.Plan.RootCause) and live diagnose's strategy
// terminal must name the SAME root-cause component. A divergence here
// means a green CI run does not validate the path diagnose actually runs.
func TestEngineParity_SimulateVsDiagnose(t *testing.T) {
	m, reg := loadStorefront(t)

	// Both engines now decide health through one canonical verdict
	// (strategy.ComponentDefinitivelyUnhealthy), so they must agree for
	// every fact set. The "store unavailable" case formerly diverged —
	// simulate keyed off the state machine, diagnose off the healthy
	// predicate — and is kept here as the regression guard for that fix.
	cases := []struct {
		name      string
		overrides map[string]map[string]any
		want      string
	}{
		{"all healthy", nil, ""},
		{"store unavailable", map[string]map[string]any{"store": {"available": false, "connection_count": 0}}, "store"},
		{"store overloaded", map[string]map[string]any{"store": {"available": true, "connection_count": 600}}, "store"},
		{"api down", map[string]map[string]any{"api": {"ready_replicas": 0, "desired_replicas": 3, "restart_count": 12, "endpoints": 0}}, "api"},
		{"frontend down", map[string]map[string]any{"frontend": {"ready_replicas": 0, "desired_replicas": 2, "restart_count": 12, "endpoints": 0}}, "frontend"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := healthyStore(tc.overrides)

			simulateRoot := Plan(m, reg, store, "").RootCause
			diagnoseRoot := bfsRootComponent(t, m, reg, store)

			if simulateRoot != diagnoseRoot {
				t.Fatalf("ENGINE DIVERGENCE: simulate=%q diagnose=%q (want both %q) — the 'one engine' promise is broken for this fact set", simulateRoot, diagnoseRoot, tc.want)
			}
			if simulateRoot != tc.want {
				t.Errorf("root cause = %q, want %q", simulateRoot, tc.want)
			}
		})
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// While-guard tests
// ---------------------------------------------------------------------------

// buildWhileGuardModel constructs a model:
//
//	edge → api → store              (always active)
//	              api → vault       (while: vault.state == starting)
//
// The "test" provider defines a minimal "service" type with fact-based states:
//   - "live"     when available == true
//   - "starting" when available == false
func buildWhileGuardModel() (*model.Model, *providersupport.Registry) {
	// Build a minimal provider with a "service" type.
	reg := providersupport.NewRegistry()

	liveCond, _ := expr.Parse("available == true")
	startingCond, _ := expr.Parse("available == false")

	reg.Register(&providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "test"},
		Types: map[string]*providersupport.Type{
			"service": {
				Name:               "service",
				DefaultActiveState: "live",
				Facts:              map[string]*providersupport.FactSpec{},
				States: []providersupport.StateDef{
					{Name: "live", When: liveCond},
					{Name: "starting", When: startingCond},
				},
			},
		},
	})

	// Parse the while guard expression: vault.state == starting
	// This checks the DERIVED state of vault (populated by state.Derive).
	whileExpr, _ := expr.Parse("vault.state == starting")

	m := &model.Model{
		Meta: model.Meta{
			Name:      "while-guard-test",
			Version:   "1",
			Providers: []string{"test"},
		},
		Components: map[string]*model.Component{
			"edge": {
				Name: "edge",
				Type: "service",
				Depends: []model.Dependency{
					{On: []string{"api"}},
				},
			},
			"api": {
				Name: "api",
				Type: "service",
				Depends: []model.Dependency{
					{On: []string{"store"}},
					{On: []string{"vault"}, While: whileExpr},
				},
			},
			"store": {
				Name: "store",
				Type: "service",
			},
			"vault": {
				Name: "vault",
				Type: "service",
			},
		},
		Order: []string{"edge", "api", "store", "vault"},
	}
	m.BuildGraph()

	return m, reg
}

// TestPlan_WhileGuard_Inactive verifies that when vault is "live",
// the api→vault edge (guarded by "vault.state == starting") is inactive
// and vault does not appear in the enumerated paths.
func TestPlan_WhileGuard_Inactive(t *testing.T) {
	m, reg := buildWhileGuardModel()

	// Vault available=true → state derives to "live" → while guard
	// "vault.state == starting" evaluates to false → edge skipped.
	store := facts.NewInMemory()
	store.Append("vault", facts.Fact{Key: "available", Value: true, At: time.Now()})

	tree := Plan(m, reg, store, "")

	// Collect all components that appear in any path (alive + eliminated).
	allPaths := append(append([]Path{}, tree.Paths...), tree.Eliminated...)
	found := map[string]bool{}
	for _, p := range allPaths {
		for _, c := range p.Components {
			found[c] = true
		}
	}

	if found["vault"] {
		t.Errorf("vault should NOT appear in paths when while guard is inactive (vault is live), but it was found")
		t.Logf("alive paths:")
		for _, p := range tree.Paths {
			t.Logf("  %v", p.Components)
		}
		t.Logf("eliminated paths:")
		for _, p := range tree.Eliminated {
			t.Logf("  %v", p.Components)
		}
	}

	// store should still be reachable (always-active edge).
	if !found["store"] {
		t.Errorf("store should appear in paths (always-active dependency), but was not found")
	}
}

// TestPlan_WhileGuard_Active verifies that when vault is "starting",
// the api→vault edge (guarded by "vault.state == starting") is active
// and vault appears in the enumerated paths.
func TestPlan_WhileGuard_Active(t *testing.T) {
	m, reg := buildWhileGuardModel()

	// Vault available=false → state derives to "starting" → while guard
	// "vault.state == starting" evaluates to true → edge walked.
	store := facts.NewInMemory()
	store.Append("vault", facts.Fact{Key: "available", Value: false, At: time.Now()})

	tree := Plan(m, reg, store, "")

	allPaths := append(append([]Path{}, tree.Paths...), tree.Eliminated...)
	found := map[string]bool{}
	for _, p := range allPaths {
		for _, c := range p.Components {
			found[c] = true
		}
	}

	if !found["vault"] {
		t.Errorf("vault should appear in paths when while guard is active (vault is starting), but was not found")
		t.Logf("alive paths:")
		for _, p := range tree.Paths {
			t.Logf("  %v", p.Components)
		}
		t.Logf("eliminated paths:")
		for _, p := range tree.Eliminated {
			t.Logf("  %v", p.Components)
		}
	}

	if !found["store"] {
		t.Errorf("store should appear in paths (always-active dependency), but was not found")
	}
}

// TestPlan_WhileGuard_Unresolved verifies that when the while guard
// references a fact that hasn't been collected, the edge is conservatively
// walked (UnresolvedError → treat as active).
func TestPlan_WhileGuard_Unresolved(t *testing.T) {
	// Build a variant model where the while guard references a fact, not state:
	//   api → vault  (while: vault.ready == true)
	// When vault.ready is missing → (false, *UnresolvedError) → walk edge.
	whileFactExpr, _ := expr.Parse("vault.ready == true")

	liveCond, _ := expr.Parse("state == live")
	startingCond, _ := expr.Parse("state == starting")

	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "test"},
		Types: map[string]*providersupport.Type{
			"service": {
				Name:               "service",
				DefaultActiveState: "live",
				Facts:              map[string]*providersupport.FactSpec{},
				States: []providersupport.StateDef{
					{Name: "live", When: liveCond},
					{Name: "starting", When: startingCond},
				},
			},
		},
	})

	m := &model.Model{
		Meta: model.Meta{
			Name:      "while-unresolved-test",
			Version:   "1",
			Providers: []string{"test"},
		},
		Components: map[string]*model.Component{
			"edge": {
				Name: "edge",
				Type: "service",
				Depends: []model.Dependency{
					{On: []string{"api"}},
				},
			},
			"api": {
				Name: "api",
				Type: "service",
				Depends: []model.Dependency{
					{On: []string{"vault"}, While: whileFactExpr},
				},
			},
			"vault": {
				Name: "vault",
				Type: "service",
			},
		},
		Order: []string{"edge", "api", "vault"},
	}
	m.BuildGraph()

	// No "ready" fact for vault → UnresolvedError → conservative walk.
	store := facts.NewInMemory()

	tree := Plan(m, reg, store, "")

	allPaths := append(append([]Path{}, tree.Paths...), tree.Eliminated...)
	found := map[string]bool{}
	for _, p := range allPaths {
		for _, c := range p.Components {
			found[c] = true
		}
	}

	if !found["vault"] {
		t.Errorf("vault should appear in paths when while guard is unresolved (conservative walk), but was not found")
	}
}
