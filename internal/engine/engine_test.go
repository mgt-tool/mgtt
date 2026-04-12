package engine

import (
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"mgtt/internal/facts"
	"mgtt/internal/model"
	"mgtt/internal/provider"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func loadStorefront(t *testing.T) (*model.Model, *provider.Registry) {
	t.Helper()
	root := repoRoot()
	m, err := model.Load(filepath.Join(root, "examples", "storefront", "system.model.yaml"))
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	reg := provider.NewRegistry()
	for _, name := range []string{"kubernetes", "aws"} {
		p, err := provider.LoadFromFile(filepath.Join(root, "providers", name, "provider.yaml"))
		if err != nil {
			t.Fatalf("load provider %s: %v", name, err)
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
