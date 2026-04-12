package model

import "mgtt/internal/expr"

// Model is the in-memory representation of a system.model.yaml file after
// loading and graph construction.
type Model struct {
	Meta       Meta
	Components map[string]*Component
	Order      []string  // declaration order from YAML
	graph      *depGraph // unexported, built during Load / BuildGraph
}

// Meta holds the top-level metadata block.
type Meta struct {
	Name      string
	Version   string
	Providers []string
	Vars      map[string]string
}

// Component represents a single infrastructure component.
type Component struct {
	Name         string
	Type         string
	Providers    []string            // nil → inherit Meta.Providers
	Depends      []Dependency
	HealthyRaw   []string            // raw expression strings, compiled in Phase 2
	Healthy      []expr.Node         // compiled from HealthyRaw
	FailureModes map[string][]string // state → can_cause list
}

// Dependency captures a single depends-on clause with an optional while guard.
type Dependency struct {
	On       []string
	WhileRaw string    // raw expression string, compiled in Phase 2
	While    expr.Node // compiled from WhileRaw; nil means always active
}

// ValidationResult accumulates errors and warnings from all validation passes.
type ValidationResult struct {
	Errors   []ValidationError
	Warnings []ValidationWarning
}

// ValidationError is a fatal validation finding.
type ValidationError struct {
	Component  string
	Field      string
	Message    string
	Suggestion string
}

// ValidationWarning is a non-fatal validation finding.
type ValidationWarning struct {
	Component string
	Field     string
	Message   string
}

// HasErrors reports whether any errors were recorded.
func (v *ValidationResult) HasErrors() bool { return len(v.Errors) > 0 }

// BuildGraph constructs the internal dependency graph from the loaded
// components. It is called automatically by Load; callers that construct a
// Model manually should call it before calling EntryPoint or DependenciesOf.
func (m *Model) BuildGraph() {
	m.graph = NewDepGraph(m.Components, m.Order)
}

// EntryPoint returns the first component (in declaration order) with
// in-degree 0, i.e., the component that nothing depends on — the top of the
// stack.
func (m *Model) EntryPoint() string {
	if m.graph == nil {
		m.BuildGraph()
	}
	return m.graph.EntryPoint()
}

// DependenciesOf returns the unfiltered list of dependency target names
// (ignores while-guard conditions). The engine's enumeratePaths iterates
// Component.Depends directly to evaluate while guards per edge.
func (m *Model) DependenciesOf(name string) []string {
	if m.graph == nil {
		m.BuildGraph()
	}
	return m.graph.DependenciesOf(name)
}
