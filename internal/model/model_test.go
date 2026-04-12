package model

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sajonaro/mgtt/internal/providersupport"
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
			Providers: []string{"kubernetes"},
			Vars:      map[string]string{"ns": "default"},
		},
		Components: map[string]*Component{
			"svc": {
				Name:         "svc",
				Type:         "deployment",
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
	if len(m.Meta.Providers) != 1 || m.Meta.Providers[0] != "kubernetes" {
		t.Errorf("Meta.Providers = %v, want [kubernetes]", m.Meta.Providers)
	}
	if m.Meta.Vars["ns"] != "default" {
		t.Errorf("Meta.Vars[ns] = %q, want %q", m.Meta.Vars["ns"], "default")
	}

	svc, ok := m.Components["svc"]
	if !ok {
		t.Fatal("Components[svc] missing")
	}
	if svc.Type != "deployment" {
		t.Errorf("svc.Type = %q, want %q", svc.Type, "deployment")
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
	// nginx → frontend → api → rds (linear chain with nginx at top)
	// nginx depends on frontend and api; frontend depends on api; api depends on rds
	components := map[string]*Component{
		"nginx":    {Name: "nginx", Type: "ingress", Depends: []Dependency{{On: []string{"frontend"}}, {On: []string{"api"}}}},
		"frontend": {Name: "frontend", Type: "deployment", Depends: []Dependency{{On: []string{"api"}}}},
		"api":      {Name: "api", Type: "deployment", Depends: []Dependency{{On: []string{"rds"}}}},
		"rds":      {Name: "rds", Type: "rds_instance"},
	}
	order := []string{"nginx", "frontend", "api", "rds"}
	g := NewDepGraph(components, order)

	ep := g.EntryPoint()
	if ep != "nginx" {
		t.Errorf("EntryPoint() = %q, want %q", ep, "nginx")
	}
}

// ---------------------------------------------------------------------------
// TestDepGraph_DependenciesOf
// ---------------------------------------------------------------------------

func TestDepGraph_DependenciesOf(t *testing.T) {
	components := map[string]*Component{
		"nginx":    {Name: "nginx", Type: "ingress", Depends: []Dependency{{On: []string{"frontend"}}, {On: []string{"api"}}}},
		"frontend": {Name: "frontend", Type: "deployment"},
		"api":      {Name: "api", Type: "deployment"},
	}
	order := []string{"nginx", "frontend", "api"}
	g := NewDepGraph(components, order)

	deps := g.DependenciesOf("nginx")
	if len(deps) != 2 {
		t.Errorf("DependenciesOf(nginx) len = %d, want 2", len(deps))
	}
	depSet := map[string]bool{}
	for _, d := range deps {
		depSet[d] = true
	}
	if !depSet["frontend"] || !depSet["api"] {
		t.Errorf("DependenciesOf(nginx) = %v, want [frontend api]", deps)
	}
}

// ---------------------------------------------------------------------------
// TestDepGraph_DetectCycle
// ---------------------------------------------------------------------------

func TestDepGraph_DetectCycle(t *testing.T) {
	// a → b → c → a
	components := map[string]*Component{
		"a": {Name: "a", Type: "deployment", Depends: []Dependency{{On: []string{"b"}}}},
		"b": {Name: "b", Type: "deployment", Depends: []Dependency{{On: []string{"c"}}}},
		"c": {Name: "c", Type: "deployment", Depends: []Dependency{{On: []string{"a"}}}},
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
		"a": {Name: "a", Type: "deployment", Depends: []Dependency{{On: []string{"b"}}}},
		"b": {Name: "b", Type: "deployment", Depends: []Dependency{{On: []string{"c"}}}},
		"c": {Name: "c", Type: "deployment"},
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
	m, err := Load(testdataPath("storefront.valid.yaml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// 4 components
	if len(m.Components) != 4 {
		t.Errorf("len(Components) = %d, want 4", len(m.Components))
	}

	// nginx exists and has 2 deps
	nginx, ok := m.Components["nginx"]
	if !ok {
		t.Fatal("nginx component missing")
	}
	nginxDeps := 0
	for _, dep := range nginx.Depends {
		nginxDeps += len(dep.On)
	}
	if nginxDeps != 2 {
		t.Errorf("nginx dep count = %d, want 2", nginxDeps)
	}

	// rds providers=[aws]
	rds, ok := m.Components["rds"]
	if !ok {
		t.Fatal("rds component missing")
	}
	if len(rds.Providers) != 1 || rds.Providers[0] != "aws" {
		t.Errorf("rds.Providers = %v, want [aws]", rds.Providers)
	}

	// rds healthy=["connection_count < 500"]
	if len(rds.HealthyRaw) != 1 || rds.HealthyRaw[0] != "connection_count < 500" {
		t.Errorf("rds.HealthyRaw = %v, want [connection_count < 500]", rds.HealthyRaw)
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
	m, err := Load(testdataPath("storefront.valid.yaml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	ep := m.EntryPoint()
	if ep != "nginx" {
		t.Errorf("EntryPoint() = %q, want %q", ep, "nginx")
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
	m, err := Load(testdataPath("storefront.valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rds := m.Components["rds"]
	if len(rds.Healthy) != 1 {
		t.Fatalf("expected 1 compiled healthy expression for rds, got %d", len(rds.Healthy))
	}
}

// ---------------------------------------------------------------------------
// TestValidate_ValidModel
// ---------------------------------------------------------------------------

func TestValidate_ValidModel(t *testing.T) {
	m, err := Load(testdataPath("storefront.valid.yaml"))
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

	// Should have an error mentioning nginx and depends
	found := false
	for _, e := range result.Errors {
		if e.Component == "nginx" && e.Field == "depends" {
			found = true
			t.Logf("found expected error: %q suggestion=%q", e.Message, e.Suggestion)
		}
	}
	if !found {
		t.Errorf("expected error on nginx.depends, errors=%v", result.Errors)
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
	m, err := Load(testdataPath("storefront.valid.yaml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	_, file, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..")

	reg := providersupport.NewRegistry()
	for _, name := range []string{"kubernetes", "aws"} {
		p, err := providersupport.LoadFromFile(filepath.Join(repoRoot, "providers", name, "provider.yaml"))
		if err != nil {
			t.Fatalf("LoadFromFile(%s): %v", name, err)
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
			Providers: []string{"kubernetes"},
		},
		Components: map[string]*Component{
			"svc": {Name: "svc", Type: "nonexistent_type"},
		},
		Order: []string{"svc"},
	}
	m.BuildGraph()

	_, file, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..")

	reg := providersupport.NewRegistry()
	p, err := providersupport.LoadFromFile(filepath.Join(repoRoot, "providers", "kubernetes", "provider.yaml"))
	if err != nil {
		t.Fatalf("LoadFromFile(kubernetes): %v", err)
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
