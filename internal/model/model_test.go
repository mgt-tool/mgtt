package model

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// testdataPath returns the absolute path to the testdata/models directory,
// anchored to the repo root regardless of where tests are run from.
func testdataPath(name string) string {
	_, filename, _, _ := runtime.Caller(0)
	// filename is .../internal/model/model_test.go
	// repo root is three levels up
	root := filepath.Join(filepath.Dir(filename), "..", "..")
	return filepath.Join(root, "testdata", "models", name)
}

// ---------------------------------------------------------------------------
// TestModelTypes: manually construct a Model and verify field access.
// ---------------------------------------------------------------------------

func TestModelTypes(t *testing.T) {
	m := &Model{
		Meta: Meta{
			Name:      "testapp",
			Version:   "2.0",
			Providers: []string{"compute"},
			Vars:      map[string]string{"ns": "default"},
		},
		Components: map[string]*Component{
			"svc": {
				Name:         "svc",
				Type:         "workload",
				Providers:    nil,
				Depends:      []Dependency{{On: []string{"db"}, WhileRaw: "load > 0"}},
				HealthyRaw:   []string{"cpu < 80"},
				FailureModes: map[string][]string{"degraded": {"svc", "cache"}},
			},
		},
		Order: []string{"svc"},
	}

	if m.Meta.Name != "testapp" {
		t.Errorf("Meta.Name = %q, want %q", m.Meta.Name, "testapp")
	}
	if m.Meta.Version != "2.0" {
		t.Errorf("Meta.Version = %q, want %q", m.Meta.Version, "2.0")
	}
	if len(m.Meta.Providers) != 1 || m.Meta.Providers[0] != "compute" {
		t.Errorf("Meta.Providers = %v, want [compute]", m.Meta.Providers)
	}
	if m.Meta.Vars["ns"] != "default" {
		t.Errorf("Meta.Vars[ns] = %q, want %q", m.Meta.Vars["ns"], "default")
	}

	svc, ok := m.Components["svc"]
	if !ok {
		t.Fatal("Components[svc] missing")
	}
	if svc.Type != "workload" {
		t.Errorf("svc.Type = %q, want %q", svc.Type, "workload")
	}
	if len(svc.Depends) != 1 || svc.Depends[0].On[0] != "db" {
		t.Errorf("svc.Depends = %v, want dep on db", svc.Depends)
	}
	if svc.Depends[0].WhileRaw != "load > 0" {
		t.Errorf("WhileRaw = %q, want %q", svc.Depends[0].WhileRaw, "load > 0")
	}
	if len(svc.HealthyRaw) != 1 || svc.HealthyRaw[0] != "cpu < 80" {
		t.Errorf("HealthyRaw = %v, want [cpu < 80]", svc.HealthyRaw)
	}

	vr := &ValidationResult{}
	if vr.HasErrors() {
		t.Error("empty ValidationResult should have no errors")
	}
	vr.Errors = append(vr.Errors, ValidationError{Component: "svc", Field: "type", Message: "missing"})
	if !vr.HasErrors() {
		t.Error("ValidationResult with error should HasErrors() == true")
	}
}

// ---------------------------------------------------------------------------
// TestDepGraph_EntryPoint
// ---------------------------------------------------------------------------

func TestDepGraph_EntryPoint(t *testing.T) {
	// edge → frontend → api → store (linear chain with edge at top)
	// edge depends on frontend and api; frontend depends on api; api depends on store
	components := map[string]*Component{
		"edge":     {Name: "edge", Type: "gateway", Depends: []Dependency{{On: []string{"frontend"}}, {On: []string{"api"}}}},
		"frontend": {Name: "frontend", Type: "workload", Depends: []Dependency{{On: []string{"api"}}}},
		"api":      {Name: "api", Type: "workload", Depends: []Dependency{{On: []string{"store"}}}},
		"store":    {Name: "store", Type: "datastore"},
	}
	order := []string{"edge", "frontend", "api", "store"}
	g := NewDepGraph(components, order)

	ep := g.EntryPoint()
	if ep != "edge" {
		t.Errorf("EntryPoint() = %q, want %q", ep, "edge")
	}
}

// ---------------------------------------------------------------------------
// TestDepGraph_DependenciesOf
// ---------------------------------------------------------------------------

func TestDepGraph_DependenciesOf(t *testing.T) {
	components := map[string]*Component{
		"edge":     {Name: "edge", Type: "gateway", Depends: []Dependency{{On: []string{"frontend"}}, {On: []string{"api"}}}},
		"frontend": {Name: "frontend", Type: "workload"},
		"api":      {Name: "api", Type: "workload"},
	}
	order := []string{"edge", "frontend", "api"}
	g := NewDepGraph(components, order)

	deps := g.DependenciesOf("edge")
	if len(deps) != 2 {
		t.Errorf("DependenciesOf(edge) len = %d, want 2", len(deps))
	}
	depSet := map[string]bool{}
	for _, d := range deps {
		depSet[d] = true
	}
	if !depSet["frontend"] || !depSet["api"] {
		t.Errorf("DependenciesOf(edge) = %v, want [frontend api]", deps)
	}
}

// ---------------------------------------------------------------------------
// TestDepGraph_DetectCycle
// ---------------------------------------------------------------------------

func TestDepGraph_DetectCycle(t *testing.T) {
	// a → b → c → a
	components := map[string]*Component{
		"a": {Name: "a", Type: "workload", Depends: []Dependency{{On: []string{"b"}}}},
		"b": {Name: "b", Type: "workload", Depends: []Dependency{{On: []string{"c"}}}},
		"c": {Name: "c", Type: "workload", Depends: []Dependency{{On: []string{"a"}}}},
	}
	order := []string{"a", "b", "c"}
	g := NewDepGraph(components, order)

	cycle := g.DetectCycle()
	if len(cycle) == 0 {
		t.Error("DetectCycle() returned nil/empty, want a cycle path")
	}
	t.Logf("cycle path: %v", cycle)
}

// ---------------------------------------------------------------------------
// TestDepGraph_NoCycle
// ---------------------------------------------------------------------------

func TestDepGraph_NoCycle(t *testing.T) {
	// a → b → c (linear, no cycle)
	components := map[string]*Component{
		"a": {Name: "a", Type: "workload", Depends: []Dependency{{On: []string{"b"}}}},
		"b": {Name: "b", Type: "workload", Depends: []Dependency{{On: []string{"c"}}}},
		"c": {Name: "c", Type: "workload"},
	}
	order := []string{"a", "b", "c"}
	g := NewDepGraph(components, order)

	cycle := g.DetectCycle()
	if len(cycle) != 0 {
		t.Errorf("DetectCycle() = %v, want nil (no cycle)", cycle)
	}
}

// ---------------------------------------------------------------------------
// TestLoad_StorefrontModel
// ---------------------------------------------------------------------------

func TestLoad_StorefrontModel(t *testing.T) {
	m, err := Load(testdataPath("sample-model.yaml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// 4 components
	if len(m.Components) != 4 {
		t.Errorf("len(Components) = %d, want 4", len(m.Components))
	}

	// edge exists and has 2 deps
	edge, ok := m.Components["edge"]
	if !ok {
		t.Fatal("edge component missing")
	}
	nginxDeps := 0
	for _, dep := range edge.Depends {
		nginxDeps += len(dep.On)
	}
	if nginxDeps != 2 {
		t.Errorf("edge dep count = %d, want 2", nginxDeps)
	}

	// store providers=[datalayer]
	store, ok := m.Components["store"]
	if !ok {
		t.Fatal("store component missing")
	}
	if len(store.Providers) != 1 || store.Providers[0] != "datalayer" {
		t.Errorf("store.Providers = %v, want [datalayer]", store.Providers)
	}

	// store healthy=["connection_count < 500"]
	if len(store.HealthyRaw) != 1 || store.HealthyRaw[0] != "connection_count < 500" {
		t.Errorf("store.HealthyRaw = %v, want [connection_count < 500]", store.HealthyRaw)
	}

	// Order has 4 entries
	if len(m.Order) != 4 {
		t.Errorf("len(Order) = %d, want 4", len(m.Order))
	}
}

// ---------------------------------------------------------------------------
// TestLoad_EntryPoint
// ---------------------------------------------------------------------------

func TestLoad_EntryPoint(t *testing.T) {
	m, err := Load(testdataPath("sample-model.yaml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	ep := m.EntryPoint()
	if ep != "edge" {
		t.Errorf("EntryPoint() = %q, want %q", ep, "edge")
	}
}

// ---------------------------------------------------------------------------
// TestLoad_FileNotFound
// ---------------------------------------------------------------------------

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load(testdataPath("does-not-exist.yaml"))
	if err == nil {
		t.Error("Load() should return error for missing file")
	}
}

// ---------------------------------------------------------------------------
// TestLoad_HealthyCompiled
// ---------------------------------------------------------------------------

func TestLoad_HealthyCompiled(t *testing.T) {
	m, err := Load(testdataPath("sample-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	store := m.Components["store"]
	if len(store.Healthy) != 1 {
		t.Fatalf("expected 1 compiled healthy expression for store, got %d", len(store.Healthy))
	}
}

// ---------------------------------------------------------------------------
// TestValidate_ValidModel
// ---------------------------------------------------------------------------

func TestValidate_ValidModel(t *testing.T) {
	m, err := Load(testdataPath("sample-model.yaml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	result := Validate(m, nil)
	if result.HasErrors() {
		for _, e := range result.Errors {
			t.Errorf("unexpected error: component=%q field=%q msg=%q", e.Component, e.Field, e.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// TestValidate_MissingDep
// ---------------------------------------------------------------------------

func TestValidate_MissingDep(t *testing.T) {
	m, err := Load(testdataPath("missing-dep.yaml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	result := Validate(m, nil)
	if !result.HasErrors() {
		t.Fatal("expected validation errors for missing dep, got none")
	}

	// Should have an error mentioning edge and depends
	found := false
	for _, e := range result.Errors {
		if e.Component == "edge" && e.Field == "depends" {
			found = true
			t.Logf("found expected error: %q suggestion=%q", e.Message, e.Suggestion)
		}
	}
	if !found {
		t.Errorf("expected error on edge.depends, errors=%v", result.Errors)
	}
}

// ---------------------------------------------------------------------------
// TestValidate_CircularDep
// ---------------------------------------------------------------------------

func TestValidate_CircularDep(t *testing.T) {
	m, err := Load(testdataPath("circular.yaml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	result := Validate(m, nil)
	if !result.HasErrors() {
		t.Fatal("expected cycle error, got none")
	}

	found := false
	for _, e := range result.Errors {
		if e.Field == "depends" && e.Component == "" {
			found = true
			t.Logf("cycle error: %q", e.Message)
		}
	}
	if !found {
		t.Errorf("expected cycle error (component='', field='depends'), errors=%v", result.Errors)
	}
}

// ---------------------------------------------------------------------------
// TestValidate_MissingType
// ---------------------------------------------------------------------------

func TestValidate_MissingType(t *testing.T) {
	m := &Model{
		Meta: Meta{
			Name:    "test",
			Version: "1.0",
		},
		Components: map[string]*Component{
			"svc": {Name: "svc", Type: ""},
		},
		Order: []string{"svc"},
	}
	m.BuildGraph()

	result := Validate(m, nil)
	if !result.HasErrors() {
		t.Fatal("expected structural error for missing type, got none")
	}

	found := false
	for _, e := range result.Errors {
		if e.Component == "svc" && e.Field == "type" {
			found = true
			t.Logf("found expected error: %q", e.Message)
		}
	}
	if !found {
		t.Errorf("expected error on svc.type, errors=%v", result.Errors)
	}
}

// ---------------------------------------------------------------------------
// TestValidate_TypeResolution — load storefront model + providers, expect no errors
// ---------------------------------------------------------------------------

func TestValidate_TypeResolution(t *testing.T) {
	m, err := Load(testdataPath("sample-model.yaml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	reg := providersupport.NewRegistry()
	for _, pair := range []struct{ name, file string }{
		{"compute", "../../testdata/providers/compute.yaml"},
		{"datalayer", "../../testdata/providers/datalayer.yaml"},
	} {
		p, err := providersupport.LoadFromFile(pair.file)
		if err != nil {
			t.Fatalf("LoadFromFile(%s): %v", pair.name, err)
		}
		reg.Register(p)
	}

	result := Validate(m, reg)
	if result.HasErrors() {
		for _, e := range result.Errors {
			t.Errorf("unexpected error: component=%q field=%q msg=%q", e.Component, e.Field, e.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// TestValidate_UnknownType — type that doesn't exist in any provider
// ---------------------------------------------------------------------------

func TestValidate_UnknownType(t *testing.T) {
	m := &Model{
		Meta: Meta{
			Name:      "test",
			Version:   "1.0",
			Providers: []string{"compute"},
		},
		Components: map[string]*Component{
			"svc": {Name: "svc", Type: "nonexistent_type"},
		},
		Order: []string{"svc"},
	}
	m.BuildGraph()

	reg := providersupport.NewRegistry()
	p, err := providersupport.LoadFromFile("../../testdata/providers/compute.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile(compute): %v", err)
	}
	reg.Register(p)

	result := Validate(m, reg)
	if !result.HasErrors() {
		t.Fatal("expected type resolution error for unknown type, got none")
	}

	found := false
	for _, e := range result.Errors {
		if e.Component == "svc" && e.Field == "type" {
			found = true
			t.Logf("found expected error: %q", e.Message)
		}
	}
	if !found {
		t.Errorf("expected error on svc.type (unknown type), errors=%v", result.Errors)
	}
}
