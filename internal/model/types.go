package model

import "github.com/mgt-tool/mgtt/internal/expr"

// Model is the in-memory representation of a system.model.yaml file after
// loading and graph construction.
type Model struct {
	Meta       Meta
	Components map[string]*Component
	Order      []string // declaration order from YAML

	// SourcePath is the on-disk path the model was loaded from, when
	// known. Set by Load; left empty for models constructed in tests.
	// Consumers that need a stable sidecar location (scenarios.yaml)
	// fall back to returning nil / skipping when this is empty.
	SourcePath string

	graph *depGraph // unexported, built during Load / BuildGraph
}

// Meta holds the top-level metadata block.
type Meta struct {
	Name      string
	Version   string
	Providers []string
	Vars      map[string]string
	// StrictTypes, when true, forbids the generic-provider fallback
	// during type resolution. An unknown type then becomes a validation
	// error instead of silently resolving to generic.component. Default
	// is false — the generic fallback is allowed and INFO-logged.
	StrictTypes bool
	// Scenarios carries the opt-out marker for scenarios.yaml drift
	// checking. Spec shape: `meta.scenarios: none` on empty placeholder
	// models signals "no scenarios expected"; the validate path then
	// skips the drift check + skips enumeration under --write-scenarios.
	// Any other non-empty value is reserved for future use; empty means
	// "default behavior".
	Scenarios string
}

// Component represents a single infrastructure component.
type Component struct {
	Name string
	Type string
	// Resource is the upstream resource identifier the provider should
	// probe (e.g. an AWS RDS DB instance id or a k8s resource name).
	// Empty falls back to Name at probe time, preserving the historical
	// "component key is the resource name" behavior.
	Resource     string
	Providers    []string // nil → inherit Meta.Providers
	Depends      []Dependency
	HealthyRaw   []string            // raw expression strings, compiled in Phase 2
	Healthy      []expr.Node         // compiled from HealthyRaw
	FailureModes map[string][]string // state → can_cause list
	// Vars override Meta.Vars on a per-component basis for probe
	// substitution and provider argv. Use this when a component lives
	// in a different namespace/region/etc from the model-wide default.
	// Precedence (high → low): component.Vars > Meta.Vars. Keys absent
	// here fall through to Meta.Vars unchanged.
	Vars map[string]string
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
