package engine

import (
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/sajonaro/mgtt/internal/expr"
	"github.com/sajonaro/mgtt/internal/facts"
	"github.com/sajonaro/mgtt/internal/model"
	"github.com/sajonaro/mgtt/internal/providersupport"
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
	m, err := model.Load(filepath.Join(root, "examples", "storefront", "system.model.yaml"))
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	reg := providersupport.NewRegistry()
	for _, pair := range []struct{ name, file string }{
		{"kubernetes", "testdata/kubernetes-provider.yaml"},
		{"aws", "testdata/aws-provider.yaml"},
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
	if tree.Entry != "nginx" {
		t.Errorf("entry = %q, want %q", tree.Entry, "nginx")
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

// Scenario 1: rds unavailable — rds stops, api crash-loops as a result.
// Engine should trace fault to rds, not api.
func TestPlan_RDSUnavailable(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"rds": {"available": false, "connection_count": 0},
		"api": {"ready_replicas": 0, "restart_count": 12, "desired_replicas": 3},
	})

	tree := Plan(m, reg, store, "")

	if tree.RootCause != "rds" {
		t.Errorf("root_cause = %q, want %q", tree.RootCause, "rds")
	}

	path := rootCausePath(tree)
	wantPath := []string{"nginx", "api", "rds"}
	if !sliceEqual(path, wantPath) {
		t.Errorf("root cause path = %v, want %v", path, wantPath)
	}

	elim := eliminatedComponents(tree)
	wantElim := []string{"frontend"}
	if !sliceEqual(elim, wantElim) {
		t.Errorf("eliminated = %v, want %v", elim, wantElim)
	}
}

// Scenario 2: api crash-loop independent of rds — rds is healthy.
func TestPlan_APICrashLoop(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"api": {"ready_replicas": 0, "restart_count": 24, "desired_replicas": 3},
		"rds": {"available": true, "connection_count": 120},
	})

	tree := Plan(m, reg, store, "")

	if tree.RootCause != "api" {
		t.Errorf("root_cause = %q, want %q", tree.RootCause, "api")
	}

	path := rootCausePath(tree)
	wantPath := []string{"nginx", "api"}
	if !sliceEqual(path, wantPath) {
		t.Errorf("root cause path = %v, want %v", path, wantPath)
	}

	elim := eliminatedComponents(tree)
	wantElim := []string{"frontend", "rds"}
	if !sliceEqual(elim, wantElim) {
		t.Errorf("eliminated = %v, want %v", elim, wantElim)
	}
}

// Scenario 3: frontend crash-looping, api and rds healthy.
func TestPlan_FrontendDegraded(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"frontend": {"ready_replicas": 0, "restart_count": 8, "desired_replicas": 2},
		"api":      {"ready_replicas": 3, "desired_replicas": 3, "endpoints": 3},
		"rds":      {"available": true, "connection_count": 98},
	})

	tree := Plan(m, reg, store, "")

	if tree.RootCause != "frontend" {
		t.Errorf("root_cause = %q, want %q", tree.RootCause, "frontend")
	}

	path := rootCausePath(tree)
	wantPath := []string{"nginx", "frontend"}
	if !sliceEqual(path, wantPath) {
		t.Errorf("root cause path = %v, want %v", path, wantPath)
	}

	elim := eliminatedComponents(tree)
	wantElim := []string{"api", "rds"}
	if !sliceEqual(elim, wantElim) {
		t.Errorf("eliminated = %v, want %v", elim, wantElim)
	}
}

// Scenario 4: all components healthy — no false positives.
func TestPlan_AllHealthy(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"nginx":    {"upstream_count": 4},
		"frontend": {"ready_replicas": 2, "desired_replicas": 2, "endpoints": 2},
		"api":      {"ready_replicas": 3, "desired_replicas": 3, "endpoints": 3},
		"rds":      {"available": true, "connection_count": 87},
	})

	tree := Plan(m, reg, store, "")

	if tree.RootCause != "" {
		t.Errorf("root_cause = %q, want empty", tree.RootCause)
	}

	if len(tree.Paths) != 0 {
		t.Errorf("surviving paths = %d, want 0", len(tree.Paths))
	}

	elim := eliminatedComponents(tree)
	wantElim := []string{"api", "frontend", "nginx", "rds"}
	if !sliceEqual(elim, wantElim) {
		t.Errorf("eliminated = %v, want %v", elim, wantElim)
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
//	nginx → api → rds              (always active)
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
			"nginx": {
				Name: "nginx",
				Type: "service",
				Depends: []model.Dependency{
					{On: []string{"api"}},
				},
			},
			"api": {
				Name: "api",
				Type: "service",
				Depends: []model.Dependency{
					{On: []string{"rds"}},
					{On: []string{"vault"}, While: whileExpr},
				},
			},
			"rds": {
				Name: "rds",
				Type: "service",
			},
			"vault": {
				Name: "vault",
				Type: "service",
			},
		},
		Order: []string{"nginx", "api", "rds", "vault"},
	}
	m.BuildGraph()

	return m, reg
}

// pathTerminals returns the terminal component of each path, sorted.
func pathTerminals(paths []Path) []string {
	var terminals []string
	for _, p := range paths {
		terminals = append(terminals, p.Components[len(p.Components)-1])
	}
	sort.Strings(terminals)
	return terminals
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

	// rds should still be reachable (always-active edge).
	if !found["rds"] {
		t.Errorf("rds should appear in paths (always-active dependency), but was not found")
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

	if !found["rds"] {
		t.Errorf("rds should appear in paths (always-active dependency), but was not found")
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
			"nginx": {
				Name: "nginx",
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
		Order: []string{"nginx", "api", "vault"},
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
