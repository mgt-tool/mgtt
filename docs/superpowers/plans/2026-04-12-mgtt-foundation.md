# MGTT Foundation (Phases 0–2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go module skeleton, model parser/validator, provider loader/registry, embedded kubernetes+aws providers, and CLI commands `mgtt init`, `mgtt model validate`, `mgtt provider install/ls/inspect`, `mgtt stdlib ls/inspect` — all TDD with golden-file tests.

**Architecture:** Single Go binary (`cmd/mgtt/`) with internal packages under `internal/`. Model and provider YAML parsing via `gopkg.in/yaml.v3`. Provider YAML embedded via `go:embed` with `$MGTT_HOME` filesystem override. Expression fields loaded as raw strings in this plan — compiled to AST in Plan 2. Cobra for CLI.

**Tech Stack:** Go 1.22+, cobra, gopkg.in/yaml.v3, go:embed, text/tabwriter

**Design doc:** `docs/superpowers/specs/2026-04-12-mgtt-mvp-design.md` — authoritative for all decisions. §2 decisions table is settled; do not deviate.

**Important phase boundary:** `expr` package does not exist yet. All expression fields (`healthy`, `states.when`, `depends.while`) are stored as raw strings. Model validation passes 5–7 (expression checks) are deferred to Plan 2. Provider `States[].When` and `Healthy` are raw strings, compiled later.

---

## File Map

### Phase 0 — Skeleton
| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `go.mod` | Module definition, dependencies |
| Create | `cmd/mgtt/main.go` | Entry point, cobra root, panic recovery, version |
| Create | `.github/workflows/ci.yaml` | CI: vet, test, build |
| Create | `examples/storefront/system.model.yaml` | Canonical example model |

### Phase 1 — Model parser + validator
| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `internal/model/types.go` | Model, Meta, Component, Dependency structs |
| Create | `internal/model/graph.go` | depGraph: adjacency, in-degree, entry-point detection |
| Create | `internal/model/load.go` | YAML parsing, graph construction |
| Create | `internal/model/validate.go` | Passes 1, 3, 4 (structural, dep refs, cycles) |
| Create | `internal/model/model_test.go` | Unit tests for load + validate |
| Create | `internal/render/model.go` | model validate terminal output |
| Create | `internal/render/render.go` | Shared render helpers (Deterministic flag, writer) |
| Create | `internal/cli/root.go` | Cobra root command |
| Create | `internal/cli/init.go` | `mgtt init` |
| Create | `internal/cli/model_validate.go` | `mgtt model validate` |
| Create | `testdata/models/storefront.valid.yaml` | Test copy of the valid storefront model |
| Create | `testdata/models/missing-dep.yaml` | Model with dep referencing non-existent component |
| Create | `testdata/models/circular.yaml` | Model with circular dependency |

### Phase 2 — Provider loader + registry
| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `internal/provider/types.go` | Provider, Type, FactSpec, StateDef, DataType, Range |
| Create | `internal/provider/stdlib.go` | Hardcoded Stdlib var with 10 primitives |
| Create | `internal/provider/load.go` | Provider YAML parsing + validation |
| Create | `internal/provider/registry.go` | Registry, pecking-order ResolveType, query methods |
| Create | `internal/provider/embed.go` | go:embed + $MGTT_HOME override |
| Create | `internal/provider/provider_test.go` | Tests: load, resolve, pecking order, stdlib |
| Create | `providers/kubernetes/provider.yaml` | Kubernetes ingress + deployment types |
| Create | `providers/aws/provider.yaml` | AWS rds_instance type |
| Modify | `internal/model/validate.go` | Add pass 2: type resolution against providers |
| Create | `internal/render/provider.go` | provider ls, inspect output |
| Create | `internal/render/stdlib.go` | stdlib ls, inspect output |
| Create | `internal/cli/provider_install.go` | `mgtt provider install` |
| Create | `internal/cli/provider_ls.go` | `mgtt provider ls` |
| Create | `internal/cli/provider_inspect.go` | `mgtt provider inspect` |
| Create | `internal/cli/stdlib.go` | `mgtt stdlib ls`, `mgtt stdlib inspect` |
| Create | `testdata/golden/model_validate_storefront.txt` | Expected validate output |
| Create | `testdata/golden/provider_install_kubernetes_aws.txt` | Expected install output |
| Create | `testdata/golden/provider_inspect_k8s_deployment.txt` | Expected inspect output |
| Create | `testdata/golden/stdlib_ls.txt` | Expected stdlib ls output |

---

## Task 1: Go module and directory scaffold

**Files:**
- Create: `go.mod`
- Create: `cmd/mgtt/main.go`

- [ ] **Step 1: Initialize Go module**

```bash
cd /root/docs/projects/mgtt
go mod init mgtt
go get github.com/spf13/cobra@latest
go get gopkg.in/yaml.v3@latest
```

- [ ] **Step 2: Create main.go with root command and panic recovery**

```go
// cmd/mgtt/main.go
package main

import (
	"fmt"
	"os"

	"mgtt/internal/cli"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "mgtt: internal error: %v\nThis is a bug. Please report it at https://github.com/mgtt/mgtt/issues\n", r)
			os.Exit(3)
		}
	}()
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Create root command stub**

```go
// internal/cli/root.go
package cli

import (
	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "mgtt",
	Short: "Model Guided Troubleshooting Tool",
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("mgtt version " + version)
		},
	})
}

func Execute() error {
	return rootCmd.Execute()
}
```

- [ ] **Step 4: Verify it builds and runs**

```bash
cd /root/docs/projects/mgtt
go build ./cmd/mgtt
./mgtt version
```

Expected: `mgtt version dev`

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum cmd/ internal/cli/root.go
git commit -m "feat: go module scaffold with cobra root command and version"
```

---

## Task 2: CI workflow

**Files:**
- Create: `.github/workflows/ci.yaml`

- [ ] **Step 1: Create CI workflow**

```yaml
# .github/workflows/ci.yaml
name: ci

on: [push, pull_request]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: vet
        run: go vet ./...

      - name: test
        run: go test ./...

      - name: build
        run: go build ./cmd/mgtt

      - name: version
        run: ./mgtt version
```

- [ ] **Step 2: Commit**

```bash
git add .github/
git commit -m "ci: add go vet, test, build workflow"
```

---

## Task 3: Example storefront model

**Files:**
- Create: `examples/storefront/system.model.yaml`
- Create: `testdata/models/storefront.valid.yaml` (identical copy for tests)

- [ ] **Step 1: Create the canonical storefront model**

```yaml
# examples/storefront/system.model.yaml
meta:
  name: storefront
  version: "1.0"
  providers:
    - kubernetes
  vars:
    namespace: production

components:
  nginx:
    type: ingress
    depends:
      - on: frontend
      - on: api

  frontend:
    type: deployment
    depends:
      - on: api

  api:
    type: deployment
    depends:
      - on: rds

  rds:
    providers:
      - aws
    type: rds_instance
    healthy:
      - connection_count < 500
```

- [ ] **Step 2: Copy to testdata**

```bash
mkdir -p testdata/models
cp examples/storefront/system.model.yaml testdata/models/storefront.valid.yaml
```

- [ ] **Step 3: Create test-only malformed models**

```yaml
# testdata/models/missing-dep.yaml
meta:
  name: test-missing-dep
  version: "1.0"
  providers:
    - kubernetes

components:
  nginx:
    type: ingress
    depends:
      - on: nonexistent
```

```yaml
# testdata/models/circular.yaml
meta:
  name: test-circular
  version: "1.0"
  providers:
    - kubernetes

components:
  a:
    type: deployment
    depends:
      - on: b
  b:
    type: deployment
    depends:
      - on: c
  c:
    type: deployment
    depends:
      - on: a
```

- [ ] **Step 4: Commit**

```bash
git add examples/ testdata/models/
git commit -m "feat: add storefront example model and test fixtures"
```

---

## Task 4: Model types

**Files:**
- Create: `internal/model/types.go`
- Create: `internal/model/model_test.go` (first test)

- [ ] **Step 1: Write the failing test — model types exist and are constructable**

```go
// internal/model/model_test.go
package model_test

import (
	"testing"

	"mgtt/internal/model"
)

func TestModelTypes(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{
			Name:      "test",
			Version:   "1.0",
			Providers: []string{"kubernetes"},
			Vars:      map[string]string{"namespace": "default"},
		},
		Components: map[string]*model.Component{
			"api": {
				Name: "api",
				Type: "deployment",
			},
		},
		Order: []string{"api"},
	}
	if m.Meta.Name != "test" {
		t.Fatalf("expected name 'test', got %q", m.Meta.Name)
	}
	if len(m.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(m.Components))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/model/ -v -run TestModelTypes
```

Expected: FAIL — package `model` does not exist

- [ ] **Step 3: Implement model types**

```go
// internal/model/types.go
package model

type Model struct {
	Meta       Meta
	Components map[string]*Component
	Order      []string
	graph      *depGraph
}

type Meta struct {
	Name      string
	Version   string
	Providers []string
	Vars      map[string]string
}

type Component struct {
	Name         string
	Type         string
	Providers    []string
	Depends      []Dependency
	HealthyRaw   []string
	FailureModes map[string][]string
}

type Dependency struct {
	On       []string
	WhileRaw string
}

type ValidationResult struct {
	Errors   []ValidationError
	Warnings []ValidationWarning
}

type ValidationError struct {
	Component  string
	Field      string
	Message    string
	Suggestion string
}

type ValidationWarning struct {
	Component string
	Field     string
	Message   string
}

func (v *ValidationResult) HasErrors() bool {
	return len(v.Errors) > 0
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/model/ -v -run TestModelTypes
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/model/types.go internal/model/model_test.go
git commit -m "feat(model): define Model, Component, Dependency, ValidationResult types"
```

---

## Task 5: Dependency graph

**Files:**
- Create: `internal/model/graph.go`
- Modify: `internal/model/model_test.go`

- [ ] **Step 1: Write failing tests for the graph**

Add to `internal/model/model_test.go`:

```go
func TestDepGraph_EntryPoint(t *testing.T) {
	g := model.NewDepGraph(map[string]*model.Component{
		"nginx":    {Name: "nginx", Depends: []model.Dependency{{On: []string{"api"}}}},
		"api":      {Name: "api", Depends: []model.Dependency{{On: []string{"rds"}}}},
		"rds":      {Name: "rds"},
	}, []string{"nginx", "api", "rds"})

	entry := g.EntryPoint()
	if entry != "nginx" {
		t.Fatalf("expected entry point 'nginx', got %q", entry)
	}
}

func TestDepGraph_DependenciesOf(t *testing.T) {
	g := model.NewDepGraph(map[string]*model.Component{
		"nginx": {Name: "nginx", Depends: []model.Dependency{
			{On: []string{"api"}},
			{On: []string{"frontend"}},
		}},
		"api":      {Name: "api"},
		"frontend": {Name: "frontend"},
	}, []string{"nginx", "api", "frontend"})

	deps := g.DependenciesOf("nginx")
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(deps))
	}
}

func TestDepGraph_DetectCycle(t *testing.T) {
	g := model.NewDepGraph(map[string]*model.Component{
		"a": {Name: "a", Depends: []model.Dependency{{On: []string{"b"}}}},
		"b": {Name: "b", Depends: []model.Dependency{{On: []string{"c"}}}},
		"c": {Name: "c", Depends: []model.Dependency{{On: []string{"a"}}}},
	}, []string{"a", "b", "c"})

	cycle := g.DetectCycle()
	if cycle == nil {
		t.Fatal("expected cycle to be detected")
	}
}

func TestDepGraph_NoCycle(t *testing.T) {
	g := model.NewDepGraph(map[string]*model.Component{
		"nginx": {Name: "nginx", Depends: []model.Dependency{{On: []string{"api"}}}},
		"api":   {Name: "api"},
	}, []string{"nginx", "api"})

	cycle := g.DetectCycle()
	if cycle != nil {
		t.Fatalf("expected no cycle, got %v", cycle)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/model/ -v -run TestDepGraph
```

Expected: FAIL — `NewDepGraph` not defined

- [ ] **Step 3: Implement the dependency graph**

```go
// internal/model/graph.go
package model

type depGraph struct {
	adjacency map[string][]string
	inDegree  map[string]int
	order     []string
}

func NewDepGraph(components map[string]*Component, order []string) *depGraph {
	g := &depGraph{
		adjacency: make(map[string][]string),
		inDegree:  make(map[string]int),
		order:     order,
	}
	for _, name := range order {
		g.inDegree[name] = 0
	}
	for _, name := range order {
		c := components[name]
		for _, dep := range c.Depends {
			for _, target := range dep.On {
				g.adjacency[name] = append(g.adjacency[name], target)
				g.inDegree[target]++
			}
		}
	}
	return g
}

func (g *depGraph) EntryPoint() string {
	for _, name := range g.order {
		if g.inDegree[name] == 0 {
			return name
		}
	}
	return ""
}

func (g *depGraph) DependenciesOf(name string) []string {
	return g.adjacency[name]
}

func (g *depGraph) DetectCycle() []string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int)
	parent := make(map[string]string)

	var dfs func(string) []string
	dfs = func(u string) []string {
		color[u] = gray
		for _, v := range g.adjacency[u] {
			if color[v] == gray {
				cycle := []string{v, u}
				for p := u; p != v; {
					p = parent[p]
					cycle = append(cycle, p)
				}
				return cycle
			}
			if color[v] == white {
				parent[v] = u
				if c := dfs(v); c != nil {
					return c
				}
			}
		}
		color[u] = black
		return nil
	}

	for _, name := range g.order {
		if color[name] == white {
			if c := dfs(name); c != nil {
				return c
			}
		}
	}
	return nil
}

func (g *depGraph) AllRoots() []string {
	var roots []string
	for _, name := range g.order {
		if g.inDegree[name] == 0 {
			roots = append(roots, name)
		}
	}
	return roots
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/model/ -v -run TestDepGraph
```

Expected: all 4 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/model/graph.go internal/model/model_test.go
git commit -m "feat(model): dependency graph with entry-point detection and cycle detection"
```

---

## Task 6: Model YAML loader

**Files:**
- Create: `internal/model/load.go`
- Modify: `internal/model/model_test.go`

- [ ] **Step 1: Write failing test — load the storefront model**

Add to `internal/model/model_test.go`:

```go
import "os"

func TestLoad_StorefrontModel(t *testing.T) {
	m, err := model.Load("../../testdata/models/storefront.valid.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if m.Meta.Name != "storefront" {
		t.Fatalf("expected name 'storefront', got %q", m.Meta.Name)
	}
	if m.Meta.Version != "1.0" {
		t.Fatalf("expected version '1.0', got %q", m.Meta.Version)
	}
	if len(m.Components) != 4 {
		t.Fatalf("expected 4 components, got %d", len(m.Components))
	}
	nginx := m.Components["nginx"]
	if nginx == nil {
		t.Fatal("nginx component not found")
	}
	if len(nginx.Depends) != 2 {
		t.Fatalf("nginx: expected 2 deps, got %d", len(nginx.Depends))
	}
	rds := m.Components["rds"]
	if rds == nil {
		t.Fatal("rds component not found")
	}
	if len(rds.Providers) != 1 || rds.Providers[0] != "aws" {
		t.Fatalf("rds: expected providers [aws], got %v", rds.Providers)
	}
	if len(rds.HealthyRaw) != 1 || rds.HealthyRaw[0] != "connection_count < 500" {
		t.Fatalf("rds: expected healthy raw, got %v", rds.HealthyRaw)
	}
}

func TestLoad_EntryPoint(t *testing.T) {
	m, err := model.Load("../../testdata/models/storefront.valid.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	entry := m.EntryPoint()
	if entry != "nginx" {
		t.Fatalf("expected entry 'nginx', got %q", entry)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := model.Load("nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/model/ -v -run TestLoad
```

Expected: FAIL — `model.Load` not defined

- [ ] **Step 3: Implement the YAML loader**

```go
// internal/model/load.go
package model

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type rawModel struct {
	Meta       rawMeta                `yaml:"meta"`
	Components map[string]rawComponent `yaml:"components"`
}

type rawMeta struct {
	Name      string            `yaml:"name"`
	Version   string            `yaml:"version"`
	Providers []string          `yaml:"providers"`
	Vars      map[string]string `yaml:"vars"`
}

type rawComponent struct {
	Type         string          `yaml:"type"`
	Providers    []string        `yaml:"providers"`
	Depends      []rawDependency `yaml:"depends"`
	Healthy      []string        `yaml:"healthy"`
	FailureModes map[string]struct {
		CanCause []string `yaml:"can_cause"`
	} `yaml:"failure_modes"`
}

type rawDependency struct {
	On    interface{} `yaml:"on"`
	While string      `yaml:"while"`
}

func Load(path string) (*Model, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading model: %w", err)
	}

	var raw rawModel
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing model: %w", err)
	}

	m := &Model{
		Meta: Meta{
			Name:      raw.Meta.Name,
			Version:   raw.Meta.Version,
			Providers: raw.Meta.Providers,
			Vars:      raw.Meta.Vars,
		},
		Components: make(map[string]*Component),
	}

	for name, rc := range raw.Components {
		m.Order = append(m.Order, name)
	}
	sortByYAMLOrder(data, m.Order)

	for _, name := range m.Order {
		rc := raw.Components[name]
		c := &Component{
			Name:       name,
			Type:       rc.Type,
			Providers:  rc.Providers,
			HealthyRaw: rc.Healthy,
		}
		if rc.FailureModes != nil {
			c.FailureModes = make(map[string][]string)
			for state, fm := range rc.FailureModes {
				c.FailureModes[state] = fm.CanCause
			}
		}
		for _, rd := range rc.Depends {
			dep := Dependency{WhileRaw: rd.While}
			switch v := rd.On.(type) {
			case string:
				dep.On = []string{v}
			case []interface{}:
				for _, item := range v {
					dep.On = append(dep.On, fmt.Sprintf("%v", item))
				}
			}
			c.Depends = append(c.Depends, dep)
		}
		m.Components[name] = c
	}

	m.graph = NewDepGraph(m.Components, m.Order)
	return m, nil
}

func (m *Model) EntryPoint() string {
	if m.graph == nil {
		return ""
	}
	return m.graph.EntryPoint()
}

func (m *Model) DependenciesOf(name string) []string {
	if m.graph == nil {
		return nil
	}
	return m.graph.DependenciesOf(name)
}

func sortByYAMLOrder(data []byte, names []string) {
	positions := make(map[string]int)
	for i, b := range data {
		for _, name := range names {
			if positions[name] == 0 {
				if matchesKeyAtPosition(data, i, name) {
					positions[name] = i
				}
			}
		}
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if positions[names[i]] > positions[names[j]] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
}

func matchesKeyAtPosition(data []byte, pos int, key string) bool {
	if pos > 0 && data[pos-1] != '\n' && pos != 0 {
		return false
	}
	end := pos + len(key)
	if end >= len(data) {
		return false
	}
	if string(data[pos:end]) == key && data[end] == ':' {
		return true
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/model/ -v -run TestLoad
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/model/load.go
git commit -m "feat(model): YAML loader with declaration-order preservation and graph construction"
```

---

## Task 7: Model validation — structural, dep refs, cycles

**Files:**
- Create: `internal/model/validate.go`
- Modify: `internal/model/model_test.go`

- [ ] **Step 1: Write failing tests for validation**

Add to `internal/model/model_test.go`:

```go
func TestValidate_ValidModel(t *testing.T) {
	m, err := model.Load("../../testdata/models/storefront.valid.yaml")
	if err != nil {
		t.Fatal(err)
	}
	result := model.Validate(m, nil)
	if result.HasErrors() {
		t.Fatalf("expected no errors, got %d: %v", len(result.Errors), result.Errors)
	}
}

func TestValidate_MissingDep(t *testing.T) {
	m, err := model.Load("../../testdata/models/missing-dep.yaml")
	if err != nil {
		t.Fatal(err)
	}
	result := model.Validate(m, nil)
	if !result.HasErrors() {
		t.Fatal("expected errors for missing dep")
	}
	found := false
	for _, e := range result.Errors {
		if e.Component == "nginx" && e.Field == "depends" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected error on nginx.depends referencing nonexistent")
	}
}

func TestValidate_CircularDep(t *testing.T) {
	m, err := model.Load("../../testdata/models/circular.yaml")
	if err != nil {
		t.Fatal(err)
	}
	result := model.Validate(m, nil)
	if !result.HasErrors() {
		t.Fatal("expected cycle error")
	}
	found := false
	for _, e := range result.Errors {
		if e.Field == "cycle" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cycle error, got %v", result.Errors)
	}
}

func TestValidate_MissingType(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "test", Version: "1.0"},
		Components: map[string]*model.Component{
			"api": {Name: "api", Type: ""},
		},
		Order: []string{"api"},
	}
	m.BuildGraph()
	result := model.Validate(m, nil)
	if !result.HasErrors() {
		t.Fatal("expected error for missing type")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/model/ -v -run TestValidate
```

Expected: FAIL — `model.Validate` not defined

- [ ] **Step 3: Implement validation**

```go
// internal/model/validate.go
package model

import "fmt"

func Validate(m *Model, providers interface{}) *ValidationResult {
	result := &ValidationResult{}
	validateStructural(m, result)
	validateDepRefs(m, result)
	validateCycles(m, result)
	return result
}

func validateStructural(m *Model, result *ValidationResult) {
	if m.Meta.Name == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "meta.name",
			Message: "model name is required",
		})
	}
	if m.Meta.Version == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "meta.version",
			Message: "model version is required",
		})
	}
	for _, name := range m.Order {
		c := m.Components[name]
		if c.Type == "" {
			result.Errors = append(result.Errors, ValidationError{
				Component: name,
				Field:     "type",
				Message:   "component type is required",
			})
		}
	}
}

func validateDepRefs(m *Model, result *ValidationResult) {
	for _, name := range m.Order {
		c := m.Components[name]
		for _, dep := range c.Depends {
			for _, target := range dep.On {
				if _, ok := m.Components[target]; !ok {
					result.Errors = append(result.Errors, ValidationError{
						Component:  name,
						Field:      "depends",
						Message:    fmt.Sprintf("dependency %q references unknown component", target),
						Suggestion: suggestComponent(target, m),
					})
				}
			}
		}
	}
}

func validateCycles(m *Model, result *ValidationResult) {
	if m.graph == nil {
		return
	}
	cycle := m.graph.DetectCycle()
	if cycle != nil {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "cycle",
			Message: fmt.Sprintf("circular dependency detected: %v", cycle),
		})
	}
}

func suggestComponent(name string, m *Model) string {
	for _, n := range m.Order {
		if levenshtein(name, n) <= 2 {
			return fmt.Sprintf("did you mean %q?", n)
		}
	}
	return ""
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 1; j <= lb; j++ {
		d[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, min(d[i][j-1]+1, d[i-1][j-1]+cost))
		}
	}
	return d[la][lb]
}
```

Add `BuildGraph` to types.go:

```go
func (m *Model) BuildGraph() {
	m.graph = NewDepGraph(m.Components, m.Order)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/model/ -v -run TestValidate
```

Expected: all 4 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/model/validate.go internal/model/types.go internal/model/model_test.go
git commit -m "feat(model): validation passes 1,3,4 — structural, dep refs, cycle detection"
```

---

## Task 8: Render package foundation + model validate output

**Files:**
- Create: `internal/render/render.go`
- Create: `internal/render/model.go`
- Create: `internal/render/render_test.go`

- [ ] **Step 1: Write failing test for model validate rendering**

```go
// internal/render/render_test.go
package render_test

import (
	"bytes"
	"strings"
	"testing"

	"mgtt/internal/model"
	"mgtt/internal/render"
)

func TestModelValidate_AllValid(t *testing.T) {
	result := &model.ValidationResult{}
	components := []string{"nginx", "frontend", "api", "rds"}
	depCounts := map[string]int{"nginx": 2, "frontend": 1, "api": 1, "rds": 0}

	var buf bytes.Buffer
	render.ModelValidate(&buf, result, components, depCounts)
	out := buf.String()

	if !strings.Contains(out, "nginx") {
		t.Fatal("expected 'nginx' in output")
	}
	if !strings.Contains(out, "0 errors") {
		t.Fatalf("expected '0 errors' in output, got:\n%s", out)
	}
}

func TestModelValidate_WithErrors(t *testing.T) {
	result := &model.ValidationResult{
		Errors: []model.ValidationError{
			{Component: "api", Field: "depends", Message: `dependency "db" references unknown component`, Suggestion: `did you mean "rds"?`},
		},
	}
	components := []string{"api"}
	depCounts := map[string]int{"api": 1}

	var buf bytes.Buffer
	render.ModelValidate(&buf, result, components, depCounts)
	out := buf.String()

	if !strings.Contains(out, "1 error") {
		t.Fatalf("expected '1 error' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "did you mean") {
		t.Fatalf("expected suggestion in output, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/render/ -v -run TestModelValidate
```

Expected: FAIL — package `render` does not exist

- [ ] **Step 3: Implement render**

```go
// internal/render/render.go
package render

import (
	"fmt"
	"io"
)

var Deterministic bool

func Checkmark(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}

func Pluralize(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func Writef(w io.Writer, format string, args ...interface{}) {
	fmt.Fprintf(w, format, args...)
}

func Writeln(w io.Writer, s string) {
	fmt.Fprintln(w, s)
}
```

```go
// internal/render/model.go
package render

import (
	"fmt"
	"io"

	"mgtt/internal/model"
)

func ModelValidate(w io.Writer, result *model.ValidationResult, components []string, depCounts map[string]int) {
	for _, name := range components {
		count := depCounts[name]
		hasErr := false
		for _, e := range result.Errors {
			if e.Component == name {
				hasErr = true
				break
			}
		}
		if hasErr {
			for _, e := range result.Errors {
				if e.Component == name {
					fmt.Fprintf(w, "  %s %-10s %s\n", Checkmark(false), name, e.Message)
					if e.Suggestion != "" {
						fmt.Fprintf(w, "  %13s %s\n", "", e.Suggestion)
					}
				}
			}
		} else {
			label := "valid"
			if count > 0 {
				label = fmt.Sprintf("%s valid", Pluralize(count, "dependency", "dependencies"))
			} else {
				for _, e := range result.Errors {
					if e.Component == "" && e.Field != "cycle" {
						continue
					}
				}
				label = "no dependencies"
			}
			fmt.Fprintf(w, "  %s %-10s %s\n", Checkmark(true), name, label)
		}
	}

	for _, e := range result.Errors {
		if e.Component == "" {
			fmt.Fprintf(w, "  %s %s\n", Checkmark(false), e.Message)
		}
	}
	for _, w2 := range result.Warnings {
		if w2.Component == "" {
			fmt.Fprintf(w, "  ⚠ %s\n", w2.Message)
		}
	}

	fmt.Fprintf(w, "\n  %s · %s · %s\n",
		Pluralize(len(components), "component", "components"),
		Pluralize(len(result.Errors), "error", "errors"),
		Pluralize(len(result.Warnings), "warning", "warnings"),
	)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/render/ -v -run TestModelValidate
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/render/
git commit -m "feat(render): model validate output with checkmarks and suggestions"
```

---

## Task 9: CLI init and model validate commands

**Files:**
- Create: `internal/cli/init.go`
- Create: `internal/cli/model_validate.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Create `mgtt init`**

```go
// internal/cli/init.go
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold a blank system.model.yaml",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "system.model.yaml"
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists", path)
		}
		template := `meta:
  name: my-system
  version: "1.0"
  providers:
    - kubernetes
  vars:
    namespace: default

components: {}
`
		if err := os.WriteFile(path, []byte(template), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ created %s  — edit to describe your system\n", path)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
```

- [ ] **Step 2: Create `mgtt model validate`**

```go
// internal/cli/model_validate.go
package cli

import (
	"fmt"
	"os"

	"mgtt/internal/model"
	"mgtt/internal/render"

	"github.com/spf13/cobra"
)

var modelCmd = &cobra.Command{
	Use:   "model",
	Short: "Model operations",
}

var modelValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate system.model.yaml",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "system.model.yaml"
		if len(args) > 0 {
			path = args[0]
		}

		m, err := model.Load(path)
		if err != nil {
			return err
		}

		result := model.Validate(m, nil)

		depCounts := make(map[string]int)
		for _, name := range m.Order {
			depCounts[name] = len(m.Components[name].Depends)
		}

		render.ModelValidate(cmd.OutOrStdout(), result, m.Order, depCounts)

		if result.HasErrors() {
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	modelCmd.AddCommand(modelValidateCmd)
	rootCmd.AddCommand(modelCmd)
}
```

- [ ] **Step 3: Test the CLI end-to-end**

```bash
cd /root/docs/projects/mgtt
go build ./cmd/mgtt
./mgtt model validate examples/storefront/system.model.yaml
```

Expected output (approximately):
```
  ✓ nginx      2 dependencies valid
  ✓ frontend   1 dependency valid
  ✓ api        1 dependency valid
  ✓ rds        no dependencies

  4 components · 0 errors · 0 warnings
```

- [ ] **Step 4: Test error case**

```bash
./mgtt model validate testdata/models/missing-dep.yaml
```

Expected: error output mentioning "nonexistent"

- [ ] **Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): add mgtt init and mgtt model validate commands"
```

---

## Task 10: Provider types, DataType, and stdlib

**Files:**
- Create: `internal/provider/types.go`
- Create: `internal/provider/stdlib.go`
- Create: `internal/provider/provider_test.go`

- [ ] **Step 1: Write failing test — stdlib types exist**

```go
// internal/provider/provider_test.go
package provider_test

import (
	"testing"

	"mgtt/internal/provider"
)

func TestStdlib_HasAllPrimitives(t *testing.T) {
	expected := []string{"int", "float", "bool", "string", "duration", "bytes", "ratio", "percentage", "count", "timestamp"}
	for _, name := range expected {
		dt, ok := provider.Stdlib[name]
		if !ok {
			t.Errorf("missing stdlib type %q", name)
			continue
		}
		if dt.Name != name {
			t.Errorf("stdlib type %q: Name=%q", name, dt.Name)
		}
		if dt.Base == "" {
			t.Errorf("stdlib type %q: Base is empty", name)
		}
	}
}

func TestStdlib_DurationHasUnits(t *testing.T) {
	dt := provider.Stdlib["duration"]
	if len(dt.Units) != 5 {
		t.Fatalf("duration: expected 5 units, got %d: %v", len(dt.Units), dt.Units)
	}
	if dt.Range == nil || dt.Range.Min == nil {
		t.Fatal("duration: expected Range.Min to be set")
	}
}

func TestStdlib_RatioRange(t *testing.T) {
	dt := provider.Stdlib["ratio"]
	if dt.Range == nil {
		t.Fatal("ratio: expected range")
	}
	if *dt.Range.Min != 0.0 || *dt.Range.Max != 1.0 {
		t.Fatalf("ratio: expected 0.0..1.0, got %v..%v", *dt.Range.Min, *dt.Range.Max)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/provider/ -v -run TestStdlib
```

Expected: FAIL — package `provider` does not exist

- [ ] **Step 3: Implement provider types and stdlib**

```go
// internal/provider/types.go
package provider

import "time"

type Provider struct {
	Meta      ProviderMeta
	DataTypes map[string]DataType
	Types     map[string]*Type
	Variables map[string]Variable
	Auth      AuthSpec
}

type ProviderMeta struct {
	Name        string
	Version     string
	Description string
	Requires    map[string]string
}

type DataType struct {
	Name    string
	Base    string
	Units   []string
	Range   *Range
	Default interface{}
}

type Range struct {
	Min *float64
	Max *float64
}

type Type struct {
	Name               string
	Description        string
	Facts              map[string]*FactSpec
	HealthyRaw         []string
	States             []StateDef
	DefaultActiveState string
	FailureModes       map[string][]string
}

type FactSpec struct {
	TypeName string
	TTL      time.Duration
	Probe    ProbeDef
	Default  interface{}
}

type ProbeDef struct {
	Cmd     string
	Parse   string
	Cost    string
	Access  string
	Timeout time.Duration
}

type StateDef struct {
	Name        string
	WhenRaw     string
	Description string
}

type Variable struct {
	Description string
	Required    bool
	Default     string
}

type AuthSpec struct {
	Strategy    string
	ReadsFrom   []string
	Access      AuthAccess
	Description string
}

type AuthAccess struct {
	Probes string
	Writes string
}

func ptr(f float64) *float64 {
	return &f
}
```

```go
// internal/provider/stdlib.go
package provider

var Stdlib = map[string]DataType{
	"int":        {Name: "int", Base: "int", Units: nil, Range: nil},
	"float":      {Name: "float", Base: "float", Units: nil, Range: nil},
	"bool":       {Name: "bool", Base: "bool", Units: nil, Range: nil},
	"string":     {Name: "string", Base: "string", Units: nil, Range: nil},
	"duration":   {Name: "duration", Base: "float", Units: []string{"ms", "s", "m", "h", "d"}, Range: &Range{Min: ptr(0.0)}},
	"bytes":      {Name: "bytes", Base: "int", Units: []string{"b", "kb", "mb", "gb", "tb"}, Range: &Range{Min: ptr(0.0)}},
	"ratio":      {Name: "ratio", Base: "float", Units: nil, Range: &Range{Min: ptr(0.0), Max: ptr(1.0)}},
	"percentage": {Name: "percentage", Base: "float", Units: nil, Range: &Range{Min: ptr(0.0), Max: ptr(100.0)}},
	"count":      {Name: "count", Base: "int", Units: nil, Range: &Range{Min: ptr(0.0)}},
	"timestamp":  {Name: "timestamp", Base: "string", Units: []string{"ISO8601"}, Range: nil},
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/provider/ -v -run TestStdlib
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/provider/types.go internal/provider/stdlib.go internal/provider/provider_test.go
git commit -m "feat(provider): types, DataType/Range structs, and stdlib with 10 primitives"
```

---

## Task 11: Provider YAML loader

**Files:**
- Create: `internal/provider/load.go`
- Modify: `internal/provider/provider_test.go`

- [ ] **Step 1: Write failing test — load a provider from YAML bytes**

Add to `internal/provider/provider_test.go`:

```go
func TestLoadProvider_Minimal(t *testing.T) {
	yaml := `
meta:
  name: testprov
  version: 1.0.0
  description: test provider
  requires:
    mgtt: ">=1.0"

types:
  server:
    description: a test server
    facts:
      connected:
        type: mgtt.bool
        ttl: 15s
        probe:
          cmd: "ping {name}"
          parse: exit_code
          cost: low
    healthy:
      - connected == true
    states:
      live:
        when: "connected == true"
        description: online
      stopped:
        when: "connected == false"
        description: offline
    default_active_state: live
    failure_modes:
      stopped:
        can_cause: [upstream_failure]
`
	p, err := provider.LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes failed: %v", err)
	}
	if p.Meta.Name != "testprov" {
		t.Fatalf("expected name 'testprov', got %q", p.Meta.Name)
	}
	srv, ok := p.Types["server"]
	if !ok {
		t.Fatal("type 'server' not found")
	}
	if len(srv.Facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(srv.Facts))
	}
	conn := srv.Facts["connected"]
	if conn == nil {
		t.Fatal("fact 'connected' not found")
	}
	if conn.TypeName != "mgtt.bool" {
		t.Fatalf("expected type 'mgtt.bool', got %q", conn.TypeName)
	}
	if conn.TTL.Seconds() != 15 {
		t.Fatalf("expected TTL 15s, got %v", conn.TTL)
	}
	if len(srv.States) != 2 {
		t.Fatalf("expected 2 states, got %d", len(srv.States))
	}
	if srv.DefaultActiveState != "live" {
		t.Fatalf("expected default_active_state 'live', got %q", srv.DefaultActiveState)
	}
	if len(srv.FailureModes) != 1 {
		t.Fatalf("expected 1 failure mode, got %d", len(srv.FailureModes))
	}
	causes := srv.FailureModes["stopped"]
	if len(causes) != 1 || causes[0] != "upstream_failure" {
		t.Fatalf("expected [upstream_failure], got %v", causes)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/provider/ -v -run TestLoadProvider
```

Expected: FAIL — `LoadFromBytes` not defined

- [ ] **Step 3: Implement provider YAML loader**

```go
// internal/provider/load.go
package provider

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type rawProvider struct {
	Meta      rawProviderMeta        `yaml:"meta"`
	DataTypes map[string]rawDataType `yaml:"data_types"`
	Types     map[string]rawType     `yaml:"types"`
	Variables map[string]rawVariable `yaml:"variables"`
	Auth      rawAuth                `yaml:"auth"`
}

type rawProviderMeta struct {
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description"`
	Requires    map[string]string `yaml:"requires"`
}

type rawDataType struct {
	Base    string      `yaml:"base"`
	Unit    string      `yaml:"unit"`
	Range   string      `yaml:"range"`
	Default interface{} `yaml:"default"`
}

type rawType struct {
	Description        string                    `yaml:"description"`
	Facts              map[string]rawFactSpec     `yaml:"facts"`
	Healthy            []string                   `yaml:"healthy"`
	States             yaml.Node                  `yaml:"states"`
	DefaultActiveState string                     `yaml:"default_active_state"`
	FailureModes       map[string]rawFailureMode  `yaml:"failure_modes"`
}

type rawFactSpec struct {
	Type    string      `yaml:"type"`
	TTL     string      `yaml:"ttl"`
	Probe   rawProbe    `yaml:"probe"`
	Default interface{} `yaml:"default"`
}

type rawProbe struct {
	Cmd     string `yaml:"cmd"`
	Parse   string `yaml:"parse"`
	Cost    string `yaml:"cost"`
	Access  string `yaml:"access"`
	Timeout string `yaml:"timeout"`
}

type rawFailureMode struct {
	CanCause []string `yaml:"can_cause"`
}

type rawVariable struct {
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Default     string `yaml:"default"`
}

type rawAuth struct {
	Strategy    string         `yaml:"strategy"`
	ReadsFrom   []string       `yaml:"reads_from"`
	Access      rawAuthAccess  `yaml:"access"`
	Description string         `yaml:"description"`
}

type rawAuthAccess struct {
	Probes string `yaml:"probes"`
	Writes string `yaml:"writes"`
}

func LoadFromBytes(data []byte) (*Provider, error) {
	var raw rawProvider
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing provider: %w", err)
	}

	p := &Provider{
		Meta: ProviderMeta{
			Name:        raw.Meta.Name,
			Version:     raw.Meta.Version,
			Description: raw.Meta.Description,
			Requires:    raw.Meta.Requires,
		},
		DataTypes: make(map[string]DataType),
		Types:     make(map[string]*Type),
		Variables: make(map[string]Variable),
		Auth: AuthSpec{
			Strategy:  raw.Auth.Strategy,
			ReadsFrom: raw.Auth.ReadsFrom,
			Access: AuthAccess{
				Probes: raw.Auth.Access.Probes,
				Writes: raw.Auth.Access.Writes,
			},
			Description: raw.Auth.Description,
		},
	}

	for name, rdt := range raw.DataTypes {
		p.DataTypes[name] = DataType{
			Name:    name,
			Base:    rdt.Base,
			Default: rdt.Default,
		}
	}

	for name, rt := range raw.Types {
		typ := &Type{
			Name:               name,
			Description:        rt.Description,
			Facts:              make(map[string]*FactSpec),
			HealthyRaw:         rt.Healthy,
			DefaultActiveState: rt.DefaultActiveState,
			FailureModes:       make(map[string][]string),
		}

		for fname, rfs := range rt.Facts {
			ttl, _ := time.ParseDuration(rfs.TTL)
			var timeout time.Duration
			if rfs.Probe.Timeout != "" {
				timeout, _ = time.ParseDuration(rfs.Probe.Timeout)
			}
			typ.Facts[fname] = &FactSpec{
				TypeName: rfs.Type,
				TTL:      ttl,
				Probe: ProbeDef{
					Cmd:     rfs.Probe.Cmd,
					Parse:   rfs.Probe.Parse,
					Cost:    rfs.Probe.Cost,
					Access:  rfs.Probe.Access,
					Timeout: timeout,
				},
				Default: rfs.Default,
			}
		}

		typ.States = parseStatesOrdered(&rt.States)

		for state, rfm := range rt.FailureModes {
			typ.FailureModes[state] = rfm.CanCause
		}

		p.Types[name] = typ
	}

	for name, rv := range raw.Variables {
		p.Variables[name] = Variable{
			Description: rv.Description,
			Required:    rv.Required,
			Default:     rv.Default,
		}
	}

	return p, nil
}

func LoadFromFile(path string) (*Provider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading provider: %w", err)
	}
	return LoadFromBytes(data)
}

func parseStatesOrdered(node *yaml.Node) []StateDef {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	var states []StateDef
	for i := 0; i < len(node.Content)-1; i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]

		sd := StateDef{Name: key.Value}
		if val.Kind == yaml.MappingNode {
			for j := 0; j < len(val.Content)-1; j += 2 {
				switch val.Content[j].Value {
				case "when":
					sd.WhenRaw = val.Content[j+1].Value
				case "description":
					sd.Description = val.Content[j+1].Value
				}
			}
		}
		states = append(states, sd)
	}
	return states
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/provider/ -v -run TestLoadProvider
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/provider/load.go
git commit -m "feat(provider): YAML loader with ordered state parsing and fact/probe extraction"
```

---

## Task 12: Embedded providers — kubernetes and aws YAML

**Files:**
- Create: `providers/kubernetes/provider.yaml`
- Create: `providers/aws/provider.yaml`
- Create: `internal/provider/embed.go`
- Modify: `internal/provider/provider_test.go`

- [ ] **Step 1: Write failing test — load embedded kubernetes provider**

Add to `internal/provider/provider_test.go`:

```go
func TestEmbedded_KubernetesLoads(t *testing.T) {
	p, err := provider.LoadEmbedded("kubernetes")
	if err != nil {
		t.Fatalf("LoadEmbedded kubernetes failed: %v", err)
	}
	if p.Meta.Name != "kubernetes" {
		t.Fatalf("expected name 'kubernetes', got %q", p.Meta.Name)
	}
	if _, ok := p.Types["ingress"]; !ok {
		t.Fatal("missing type 'ingress'")
	}
	if _, ok := p.Types["deployment"]; !ok {
		t.Fatal("missing type 'deployment'")
	}
	deploy := p.Types["deployment"]
	if len(deploy.States) != 4 {
		t.Fatalf("deployment: expected 4 states, got %d", len(deploy.States))
	}
	if deploy.States[0].Name != "degraded" {
		t.Fatalf("deployment: first state should be 'degraded', got %q", deploy.States[0].Name)
	}
}

func TestEmbedded_AWSLoads(t *testing.T) {
	p, err := provider.LoadEmbedded("aws")
	if err != nil {
		t.Fatalf("LoadEmbedded aws failed: %v", err)
	}
	if p.Meta.Name != "aws" {
		t.Fatalf("expected name 'aws', got %q", p.Meta.Name)
	}
	if _, ok := p.Types["rds_instance"]; !ok {
		t.Fatal("missing type 'rds_instance'")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/provider/ -v -run TestEmbedded
```

Expected: FAIL — `LoadEmbedded` not defined

- [ ] **Step 3: Create the kubernetes provider YAML**

Write the full `providers/kubernetes/provider.yaml` exactly as specified in design doc §6.2 (copy verbatim from the design doc — the full YAML starting with `meta:` through the deployment failure_modes block).

- [ ] **Step 4: Create the aws provider YAML**

Write the full `providers/aws/provider.yaml` exactly as specified in design doc §6.3.

- [ ] **Step 5: Implement embed.go**

```go
// internal/provider/embed.go
package provider

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed all:../../providers
var embedded embed.FS

func LoadEmbedded(name string) (*Provider, error) {
	home := os.Getenv("MGTT_HOME")
	if home != "" {
		overridePath := filepath.Join(home, "providers", name, "provider.yaml")
		if data, err := os.ReadFile(overridePath); err == nil {
			return LoadFromBytes(data)
		}
	}

	data, err := embedded.ReadFile(filepath.Join("../../providers", name, "provider.yaml"))
	if err != nil {
		return nil, fmt.Errorf("embedded provider %q not found: %w", name, err)
	}
	return LoadFromBytes(data)
}

func ListEmbedded() []string {
	entries, err := embedded.ReadDir("../../providers")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}
```

Note: the embed path `../../providers` is relative to the source file location (`internal/provider/embed.go`). If this doesn't resolve correctly, adjust to use `providers` with the correct relative path or restructure the embed directive. The exact path depends on Go's embed resolution rules — `go:embed` paths are relative to the source file's directory. Since `embed.go` is in `internal/provider/`, the path to `providers/` at repo root would actually need a different approach:

```go
// internal/provider/embed.go
package provider

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

// Embedded is set by the main package which has access to the repo root.
var Embedded embed.FS
var EmbeddedRoot string = "providers"

func LoadEmbedded(name string) (*Provider, error) {
	home := os.Getenv("MGTT_HOME")
	if home != "" {
		overridePath := filepath.Join(home, "providers", name, "provider.yaml")
		if data, err := os.ReadFile(overridePath); err == nil {
			return LoadFromBytes(data)
		}
	}

	path := filepath.Join(EmbeddedRoot, name, "provider.yaml")
	data, err := Embedded.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("embedded provider %q not found: %w", name, err)
	}
	return LoadFromBytes(data)
}

func ListEmbedded() []string {
	entries, err := Embedded.ReadDir(EmbeddedRoot)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}
```

And in `cmd/mgtt/main.go`, add:

```go
import (
	"embed"
	"mgtt/internal/provider"
)

//go:embed all:../../providers
var embeddedProviders embed.FS

func init() {
	provider.Embedded = embeddedProviders
}
```

Wait — `go:embed` in `cmd/mgtt/main.go` has the same relative-path problem. The fix: create a small `providers.go` file at the repo root level that's part of a `providers` package, or embed from the `cmd/mgtt/` directory using a relative path.

The cleanest approach: put the embed directive in `cmd/mgtt/main.go` with path `../../providers` — this works because `go:embed` resolves relative to the Go source file.

Update `cmd/mgtt/main.go`:

```go
package main

import (
	"embed"
	"fmt"
	"os"

	"mgtt/internal/cli"
	"mgtt/internal/provider"
)

//go:embed ../../providers/**/provider.yaml
var embeddedProviders embed.FS

func main() {
	provider.Embedded = embeddedProviders
	provider.EmbeddedRoot = "providers"

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "mgtt: internal error: %v\nThis is a bug. Please report it at https://github.com/mgtt/mgtt/issues\n", r)
			os.Exit(3)
		}
	}()
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
```

**Important:** `go:embed` does not support `..` in paths. This is a Go limitation. The workaround: move the embed directive to a file at the repo root, or restructure so `providers/` is accessible. The standard pattern is to put a small Go file at the repo root:

```go
// embed_providers.go (at repo root)
package mgtt

import "embed"

//go:embed providers/*/provider.yaml
var EmbeddedProviders embed.FS
```

Then in `cmd/mgtt/main.go`:

```go
import "mgtt"

func init() {
    provider.Embedded = mgtt.EmbeddedProviders
}
```

But this creates a circular-ish import. The cleanest Go pattern: put a standalone file in `cmd/mgtt/`:

Since Go 1.16, `go:embed` paths must not contain `..`. The solution is to put embed at the module root. Create:

```go
// embeds.go (at repo root /root/docs/projects/mgtt/)
package mgtt

import "embed"

//go:embed providers/*/provider.yaml
var EmbeddedProviders embed.FS
```

And `cmd/mgtt/main.go`:

```go
package main

import (
	"fmt"
	"os"

	root "mgtt"
	"mgtt/internal/cli"
	"mgtt/internal/provider"
)

func main() {
	provider.Embedded = root.EmbeddedProviders
	provider.EmbeddedRoot = "providers"

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "mgtt: internal error: %v\n", r)
			os.Exit(3)
		}
	}()
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
go test ./internal/provider/ -v -run TestEmbedded
```

Note: for the tests to work, you'll need to set `provider.Embedded` in a `TestMain` or use `LoadFromFile` pointing to the providers directory directly. Adjust the test to use `LoadFromFile` for now:

```go
func TestEmbedded_KubernetesLoads(t *testing.T) {
	p, err := provider.LoadFromFile("../../providers/kubernetes/provider.yaml")
	if err != nil {
		t.Fatalf("load kubernetes failed: %v", err)
	}
	// ... same assertions
}
```

The embed integration test will work once the binary is built. For unit tests, `LoadFromFile` is sufficient.

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add providers/ internal/provider/embed.go embeds.go cmd/mgtt/main.go
git commit -m "feat(provider): embedded kubernetes and aws providers with go:embed + MGTT_HOME override"
```

---

## Task 13: Provider registry with pecking-order resolution

**Files:**
- Create: `internal/provider/registry.go`
- Modify: `internal/provider/provider_test.go`

- [ ] **Step 1: Write failing tests for registry**

Add to `internal/provider/provider_test.go`:

```go
func TestRegistry_ResolveType(t *testing.T) {
	k8s, err := provider.LoadFromFile("../../providers/kubernetes/provider.yaml")
	if err != nil {
		t.Fatal(err)
	}
	aws, err := provider.LoadFromFile("../../providers/aws/provider.yaml")
	if err != nil {
		t.Fatal(err)
	}

	reg := provider.NewRegistry()
	reg.Register(k8s)
	reg.Register(aws)

	typ, owner, err := reg.ResolveType([]string{"kubernetes"}, "deployment")
	if err != nil {
		t.Fatalf("ResolveType failed: %v", err)
	}
	if owner != "kubernetes" {
		t.Fatalf("expected owner 'kubernetes', got %q", owner)
	}
	if typ.Name != "deployment" {
		t.Fatalf("expected type 'deployment', got %q", typ.Name)
	}

	typ, owner, err = reg.ResolveType([]string{"aws"}, "rds_instance")
	if err != nil {
		t.Fatal(err)
	}
	if owner != "aws" {
		t.Fatalf("expected owner 'aws', got %q", owner)
	}
}

func TestRegistry_ResolveType_NotFound(t *testing.T) {
	reg := provider.NewRegistry()
	_, _, err := reg.ResolveType([]string{"kubernetes"}, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestRegistry_PeckingOrder(t *testing.T) {
	k8s, _ := provider.LoadFromFile("../../providers/kubernetes/provider.yaml")
	reg := provider.NewRegistry()
	reg.Register(k8s)

	typ, owner, err := reg.ResolveType([]string{"kubernetes", "aws"}, "deployment")
	if err != nil {
		t.Fatal(err)
	}
	if owner != "kubernetes" {
		t.Fatalf("pecking order: expected first provider 'kubernetes', got %q", owner)
	}
	_ = typ
}

func TestRegistry_QueryMethods(t *testing.T) {
	k8s, _ := provider.LoadFromFile("../../providers/kubernetes/provider.yaml")
	reg := provider.NewRegistry()
	reg.Register(k8s)

	das, err := reg.DefaultActiveStateFor("kubernetes", "deployment")
	if err != nil {
		t.Fatal(err)
	}
	if das != "live" {
		t.Fatalf("expected 'live', got %q", das)
	}

	fm, err := reg.FailureModesFor("kubernetes", "deployment", "degraded")
	if err != nil {
		t.Fatal(err)
	}
	if len(fm) == 0 {
		t.Fatal("expected failure modes for degraded")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/provider/ -v -run "TestRegistry"
```

Expected: FAIL — `NewRegistry` not defined

- [ ] **Step 3: Implement registry**

```go
// internal/provider/registry.go
package provider

import "fmt"

type Registry struct {
	providers map[string]*Provider
	order     []string
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]*Provider),
	}
}

func (r *Registry) Register(p *Provider) {
	r.providers[p.Meta.Name] = p
	r.order = append(r.order, p.Meta.Name)
}

func (r *Registry) Get(name string) (*Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

func (r *Registry) All() []*Provider {
	var result []*Provider
	for _, name := range r.order {
		result = append(result, r.providers[name])
	}
	return result
}

func (r *Registry) ResolveType(componentProviders []string, typeName string) (*Type, string, error) {
	if provName, tName, ok := splitNamespace(typeName); ok {
		p, exists := r.providers[provName]
		if !exists {
			return nil, "", fmt.Errorf("provider %q not found", provName)
		}
		t, exists := p.Types[tName]
		if !exists {
			return nil, "", fmt.Errorf("type %q not found in provider %q", tName, provName)
		}
		return t, provName, nil
	}

	for _, provName := range componentProviders {
		p, exists := r.providers[provName]
		if !exists {
			continue
		}
		if t, ok := p.Types[typeName]; ok {
			return t, provName, nil
		}
	}

	return nil, "", fmt.Errorf("type %q not found in providers %v", typeName, componentProviders)
}

func (r *Registry) DefaultActiveStateFor(providerName, typeName string) (string, error) {
	p, ok := r.providers[providerName]
	if !ok {
		return "", fmt.Errorf("provider %q not found", providerName)
	}
	t, ok := p.Types[typeName]
	if !ok {
		return "", fmt.Errorf("type %q not found in %q", typeName, providerName)
	}
	return t.DefaultActiveState, nil
}

func (r *Registry) FailureModesFor(providerName, typeName, stateName string) ([]string, error) {
	p, ok := r.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerName)
	}
	t, ok := p.Types[typeName]
	if !ok {
		return nil, fmt.Errorf("type %q not found in %q", typeName, providerName)
	}
	fm, ok := t.FailureModes[stateName]
	if !ok {
		return nil, nil
	}
	return fm, nil
}

func (r *Registry) HealthyConditionsFor(providerName, typeName string) ([]string, error) {
	p, ok := r.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerName)
	}
	t, ok := p.Types[typeName]
	if !ok {
		return nil, fmt.Errorf("type %q not found in %q", typeName, providerName)
	}
	return t.HealthyRaw, nil
}

func (r *Registry) FactsFor(providerName, typeName string) (map[string]*FactSpec, error) {
	p, ok := r.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerName)
	}
	t, ok := p.Types[typeName]
	if !ok {
		return nil, fmt.Errorf("type %q not found in %q", typeName, providerName)
	}
	return t.Facts, nil
}

func (r *Registry) StatesFor(providerName, typeName string) ([]StateDef, error) {
	p, ok := r.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerName)
	}
	t, ok := p.Types[typeName]
	if !ok {
		return nil, fmt.Errorf("type %q not found in %q", typeName, providerName)
	}
	return t.States, nil
}

func (r *Registry) ProbeCostFor(providerName, typeName, factName string) (string, error) {
	p, ok := r.providers[providerName]
	if !ok {
		return "", fmt.Errorf("provider %q not found", providerName)
	}
	t, ok := p.Types[typeName]
	if !ok {
		return "", fmt.Errorf("type %q not found in %q", typeName, providerName)
	}
	f, ok := t.Facts[factName]
	if !ok {
		return "", fmt.Errorf("fact %q not found in %q.%q", factName, providerName, typeName)
	}
	return f.Probe.Cost, nil
}

func splitNamespace(typeName string) (string, string, bool) {
	for i, c := range typeName {
		if c == '.' {
			return typeName[:i], typeName[i+1:], true
		}
	}
	return "", "", false
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/provider/ -v -run "TestRegistry"
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/provider/registry.go
git commit -m "feat(provider): registry with pecking-order resolution and engine query methods"
```

---

## Task 14: Model validation pass 2 — type resolution

**Files:**
- Modify: `internal/model/validate.go`
- Modify: `internal/model/model_test.go`

- [ ] **Step 1: Write failing test — type resolution with providers**

Add to `internal/model/model_test.go`:

```go
import "mgtt/internal/provider"

func TestValidate_TypeResolution(t *testing.T) {
	m, err := model.Load("../../testdata/models/storefront.valid.yaml")
	if err != nil {
		t.Fatal(err)
	}

	k8s, _ := provider.LoadFromFile("../../providers/kubernetes/provider.yaml")
	aws, _ := provider.LoadFromFile("../../providers/aws/provider.yaml")
	reg := provider.NewRegistry()
	reg.Register(k8s)
	reg.Register(aws)

	result := model.Validate(m, reg)
	if result.HasErrors() {
		t.Fatalf("expected no errors with providers, got: %v", result.Errors)
	}
}

func TestValidate_UnknownType(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "test", Version: "1.0", Providers: []string{"kubernetes"}},
		Components: map[string]*model.Component{
			"api": {Name: "api", Type: "nonexistent_type"},
		},
		Order: []string{"api"},
	}
	m.BuildGraph()

	k8s, _ := provider.LoadFromFile("../../providers/kubernetes/provider.yaml")
	reg := provider.NewRegistry()
	reg.Register(k8s)

	result := model.Validate(m, reg)
	if !result.HasErrors() {
		t.Fatal("expected error for unknown type")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/model/ -v -run TestValidate_TypeResolution
```

Expected: FAIL (Validate currently ignores providers parameter)

- [ ] **Step 3: Add type resolution pass to Validate**

Update `internal/model/validate.go`:

```go
import "mgtt/internal/provider"

func Validate(m *Model, reg *provider.Registry) *ValidationResult {
	result := &ValidationResult{}
	validateStructural(m, result)
	if reg != nil {
		validateTypeResolution(m, reg, result)
	}
	validateDepRefs(m, result)
	validateCycles(m, result)
	return result
}

func validateTypeResolution(m *Model, reg *provider.Registry, result *ValidationResult) {
	for _, name := range m.Order {
		c := m.Components[name]
		providers := c.Providers
		if providers == nil {
			providers = m.Meta.Providers
		}
		_, _, err := reg.ResolveType(providers, c.Type)
		if err != nil {
			result.Errors = append(result.Errors, ValidationError{
				Component: name,
				Field:     "type",
				Message:   fmt.Sprintf("type %q could not be resolved: %v", c.Type, err),
			})
		}
	}
}
```

- [ ] **Step 4: Run all validation tests**

```bash
go test ./internal/model/ -v -run TestValidate
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/model/validate.go internal/model/model_test.go
git commit -m "feat(model): validation pass 2 — type resolution against provider registry"
```

---

## Task 15: Render — provider ls, inspect, stdlib

**Files:**
- Create: `internal/render/provider.go`
- Create: `internal/render/stdlib.go`
- Create: `internal/render/provider_test.go`

- [ ] **Step 1: Write failing tests for render output**

```go
// internal/render/provider_test.go
package render_test

import (
	"bytes"
	"strings"
	"testing"

	"mgtt/internal/provider"
	"mgtt/internal/render"
)

func TestProviderLs(t *testing.T) {
	k8s, _ := provider.LoadFromFile("../../providers/kubernetes/provider.yaml")

	var buf bytes.Buffer
	render.ProviderLs(&buf, []*provider.Provider{k8s})
	out := buf.String()

	if !strings.Contains(out, "kubernetes") {
		t.Fatalf("expected 'kubernetes' in output:\n%s", out)
	}
	if !strings.Contains(out, "1.0.0") {
		t.Fatalf("expected version in output:\n%s", out)
	}
}

func TestStdlibLs(t *testing.T) {
	var buf bytes.Buffer
	render.StdlibLs(&buf)
	out := buf.String()

	if !strings.Contains(out, "duration") {
		t.Fatalf("expected 'duration' in output:\n%s", out)
	}
	if !strings.Contains(out, "ms|s|m|h|d") {
		t.Fatalf("expected units in output:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/render/ -v -run "TestProvider|TestStdlib"
```

Expected: FAIL

- [ ] **Step 3: Implement render functions**

```go
// internal/render/provider.go
package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"mgtt/internal/provider"
)

func ProviderLs(w io.Writer, providers []*provider.Provider) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, p := range providers {
		fmt.Fprintf(tw, "  %s %s\tv%s\t%s\n", Checkmark(true), p.Meta.Name, p.Meta.Version, p.Meta.Description)
	}
	tw.Flush()
}

func ProviderInstall(w io.Writer, p *provider.Provider) {
	fmt.Fprintf(w, "  %s %-12s v%s  auth: %s  access: %s\n",
		Checkmark(true), p.Meta.Name, p.Meta.Version,
		p.Auth.Strategy, p.Auth.Access.Probes)
}

func ProviderInspect(w io.Writer, p *provider.Provider, typeName string) {
	if typeName == "" {
		fmt.Fprintf(w, "Provider: %s v%s\n", p.Meta.Name, p.Meta.Version)
		fmt.Fprintf(w, "%s\n\n", p.Meta.Description)
		fmt.Fprintln(w, "Types:")
		for name, t := range p.Types {
			fmt.Fprintf(w, "  %s — %s\n", name, t.Description)
		}
		return
	}

	t, ok := p.Types[typeName]
	if !ok {
		fmt.Fprintf(w, "type %q not found in %s\n", typeName, p.Meta.Name)
		return
	}

	fmt.Fprintf(w, "Type: %s.%s\n%s\n\n", p.Meta.Name, t.Name, t.Description)

	if len(t.Facts) > 0 {
		fmt.Fprintln(w, "Facts:")
		for name, f := range t.Facts {
			fmt.Fprintf(w, "  %-20s type: %-10s ttl: %s  cost: %s\n", name, f.TypeName, f.TTL, f.Probe.Cost)
		}
		fmt.Fprintln(w)
	}

	if len(t.HealthyRaw) > 0 {
		fmt.Fprintln(w, "Healthy:")
		for _, h := range t.HealthyRaw {
			fmt.Fprintf(w, "  - %s\n", h)
		}
		fmt.Fprintln(w)
	}

	if len(t.States) > 0 {
		fmt.Fprintln(w, "States:")
		for _, s := range t.States {
			fmt.Fprintf(w, "  %-12s when: %-45s %s\n", s.Name, s.WhenRaw, s.Description)
		}
		fmt.Fprintf(w, "  default_active_state: %s\n\n", t.DefaultActiveState)
	}

	if len(t.FailureModes) > 0 {
		fmt.Fprintln(w, "Failure modes:")
		for state, causes := range t.FailureModes {
			fmt.Fprintf(w, "  %-12s can_cause: [%s]\n", state, strings.Join(causes, ", "))
		}
	}
}
```

```go
// internal/render/stdlib.go
package render

import (
	"fmt"
	"io"
	"strings"

	"mgtt/internal/provider"
)

func StdlibLs(w io.Writer) {
	order := []string{"int", "float", "bool", "string", "duration", "bytes", "ratio", "percentage", "count", "timestamp"}
	fmt.Fprintln(w)
	for _, name := range order {
		dt := provider.Stdlib[name]
		units := "~"
		if len(dt.Units) > 0 {
			units = strings.Join(dt.Units, "|")
		}
		rangeStr := "~"
		if dt.Range != nil {
			rangeStr = formatRange(dt.Range)
		}
		fmt.Fprintf(w, "  %-12s base: %-6s unit: %-18s range: %s\n", dt.Name, dt.Base, units, rangeStr)
	}
	fmt.Fprintln(w)
}

func StdlibInspect(w io.Writer, name string) {
	dt, ok := provider.Stdlib[name]
	if !ok {
		fmt.Fprintf(w, "stdlib type %q not found\n", name)
		return
	}
	fmt.Fprintf(w, "Name:  %s\n", dt.Name)
	fmt.Fprintf(w, "Base:  %s\n", dt.Base)
	units := "~"
	if len(dt.Units) > 0 {
		units = strings.Join(dt.Units, " | ")
	}
	fmt.Fprintf(w, "Units: %s\n", units)
	rangeStr := "~"
	if dt.Range != nil {
		rangeStr = formatRange(dt.Range)
	}
	fmt.Fprintf(w, "Range: %s\n", rangeStr)
}

func formatRange(r *provider.Range) string {
	min := ""
	max := ""
	if r.Min != nil {
		if *r.Min == float64(int(*r.Min)) {
			min = fmt.Sprintf("%d", int(*r.Min))
		} else {
			min = fmt.Sprintf("%g", *r.Min)
		}
	}
	if r.Max != nil {
		if *r.Max == float64(int(*r.Max)) {
			max = fmt.Sprintf("%d", int(*r.Max))
		} else {
			max = fmt.Sprintf("%g", *r.Max)
		}
	}
	return min + ".." + max
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/render/ -v -run "TestProvider|TestStdlib"
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/render/provider.go internal/render/stdlib.go internal/render/provider_test.go
git commit -m "feat(render): provider ls, inspect, install output and stdlib ls/inspect"
```

---

## Task 16: CLI — provider install, ls, inspect, stdlib commands

**Files:**
- Create: `internal/cli/provider_install.go`
- Create: `internal/cli/provider_ls.go`
- Create: `internal/cli/provider_inspect.go`
- Create: `internal/cli/stdlib.go`

- [ ] **Step 1: Create provider command group and install**

```go
// internal/cli/provider_install.go
package cli

import (
	"fmt"

	"mgtt/internal/provider"
	"mgtt/internal/render"

	"github.com/spf13/cobra"
)

var providerCmd = &cobra.Command{
	Use:   "provider",
	Short: "Provider operations",
}

var providerInstallCmd = &cobra.Command{
	Use:   "install [names...]",
	Short: "Install providers",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, name := range args {
			p, err := provider.LoadEmbedded(name)
			if err != nil {
				return fmt.Errorf("installing %s: %w", name, err)
			}
			render.ProviderInstall(cmd.OutOrStdout(), p)
		}
		return nil
	},
}

func init() {
	providerCmd.AddCommand(providerInstallCmd)
	rootCmd.AddCommand(providerCmd)
}
```

- [ ] **Step 2: Create provider ls**

```go
// internal/cli/provider_ls.go
package cli

import (
	"mgtt/internal/provider"
	"mgtt/internal/render"

	"github.com/spf13/cobra"
)

var providerLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List installed providers",
	RunE: func(cmd *cobra.Command, args []string) error {
		names := provider.ListEmbedded()
		var providers []*provider.Provider
		for _, name := range names {
			p, err := provider.LoadEmbedded(name)
			if err != nil {
				continue
			}
			providers = append(providers, p)
		}
		render.ProviderLs(cmd.OutOrStdout(), providers)
		return nil
	},
}

func init() {
	providerCmd.AddCommand(providerLsCmd)
}
```

- [ ] **Step 3: Create provider inspect**

```go
// internal/cli/provider_inspect.go
package cli

import (
	"fmt"

	"mgtt/internal/provider"
	"mgtt/internal/render"

	"github.com/spf13/cobra"
)

var providerInspectCmd = &cobra.Command{
	Use:   "inspect <name> [type]",
	Short: "Inspect a provider",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		p, err := provider.LoadEmbedded(name)
		if err != nil {
			return fmt.Errorf("loading provider %s: %w", name, err)
		}
		typeName := ""
		if len(args) > 1 {
			typeName = args[1]
		}
		render.ProviderInspect(cmd.OutOrStdout(), p, typeName)
		return nil
	},
}

func init() {
	providerCmd.AddCommand(providerInspectCmd)
}
```

- [ ] **Step 4: Create stdlib commands**

```go
// internal/cli/stdlib.go
package cli

import (
	"mgtt/internal/provider"
	"mgtt/internal/render"

	"github.com/spf13/cobra"
)

var stdlibCmd = &cobra.Command{
	Use:   "stdlib",
	Short: "Stdlib type operations",
}

var stdlibLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all stdlib types",
	Run: func(cmd *cobra.Command, args []string) {
		render.StdlibLs(cmd.OutOrStdout())
	},
}

var stdlibInspectCmd = &cobra.Command{
	Use:   "inspect <type>",
	Short: "Inspect a stdlib type",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if _, ok := provider.Stdlib[name]; !ok {
			cmd.PrintErrf("stdlib type %q not found\n", name)
			return
		}
		render.StdlibInspect(cmd.OutOrStdout(), name)
	},
}

func init() {
	stdlibCmd.AddCommand(stdlibLsCmd)
	stdlibCmd.AddCommand(stdlibInspectCmd)
	rootCmd.AddCommand(stdlibCmd)
}
```

- [ ] **Step 5: Build and test all CLI commands**

```bash
cd /root/docs/projects/mgtt
go build ./cmd/mgtt

./mgtt provider install kubernetes aws
./mgtt provider ls
./mgtt provider inspect kubernetes deployment
./mgtt stdlib ls
./mgtt stdlib inspect duration
./mgtt model validate examples/storefront/system.model.yaml
```

Verify each produces reasonable output matching the design doc examples.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): provider install/ls/inspect and stdlib ls/inspect commands"
```

---

## Task 17: Golden-file tests for all Phase 2 commands

**Files:**
- Create: `testdata/golden/model_validate_storefront.txt`
- Create: `testdata/golden/stdlib_ls.txt`
- Create: `internal/cli/cli_test.go`

- [ ] **Step 1: Capture golden output**

```bash
cd /root/docs/projects/mgtt
./mgtt model validate examples/storefront/system.model.yaml > testdata/golden/model_validate_storefront.txt
./mgtt stdlib ls > testdata/golden/stdlib_ls.txt
```

- [ ] **Step 2: Write golden-file test harness**

```go
// internal/cli/cli_test.go
package cli_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"mgtt/internal/cli"
)

var update = false // set to true to regenerate golden files

func init() {
	for _, arg := range os.Args {
		if arg == "-update" {
			update = true
		}
	}
}

func runCommand(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	cmd := cli.RootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command %v failed: %v\noutput: %s", args, err, buf.String())
	}
	return buf.String()
}

func goldenTest(t *testing.T, goldenPath string, actual string) {
	t.Helper()
	if update {
		os.WriteFile(goldenPath, []byte(actual), 0644)
		return
	}
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file %s: %v", goldenPath, err)
	}
	if strings.TrimSpace(string(expected)) != strings.TrimSpace(actual) {
		t.Fatalf("golden mismatch for %s.\nExpected:\n%s\nActual:\n%s", goldenPath, string(expected), actual)
	}
}

func TestGolden_ModelValidateStorefront(t *testing.T) {
	out := runCommand(t, "model", "validate", "../../examples/storefront/system.model.yaml")
	goldenTest(t, "../../testdata/golden/model_validate_storefront.txt", out)
}

func TestGolden_StdlibLs(t *testing.T) {
	out := runCommand(t, "stdlib", "ls")
	goldenTest(t, "../../testdata/golden/stdlib_ls.txt", out)
}
```

Note: `cli.RootCmd()` needs to be exposed. Add to `internal/cli/root.go`:

```go
func RootCmd() *cobra.Command {
	return rootCmd
}
```

- [ ] **Step 3: Run golden tests**

```bash
go test ./internal/cli/ -v -run TestGolden
```

Expected: PASS (golden files match current output)

- [ ] **Step 4: Commit**

```bash
git add testdata/golden/ internal/cli/cli_test.go internal/cli/root.go
git commit -m "test: golden-file harness and tests for model validate and stdlib ls"
```

---

## Task 18: Final verification — all tests green, binary works

- [ ] **Step 1: Run the full test suite**

```bash
cd /root/docs/projects/mgtt
go vet ./...
go test ./...
```

Expected: all packages PASS, no vet errors.

- [ ] **Step 2: Build and run the CI recipe**

```bash
go build ./cmd/mgtt
./mgtt model validate examples/storefront/system.model.yaml
./mgtt provider install kubernetes aws
./mgtt provider ls
./mgtt provider inspect kubernetes deployment
./mgtt stdlib ls
./mgtt version
```

Verify all produce correct output.

- [ ] **Step 3: Commit any remaining fixes**

```bash
git add -A
git status
# If there are changes:
git commit -m "fix: phase 0-2 final cleanup and test fixes"
```

- [ ] **Step 4: Tag the milestone**

```bash
git tag v0.0.1-foundation
```

---

## Acceptance Criteria (Phase 0–2 complete when)

1. `go vet ./... && go test ./...` green with zero failures
2. `mgtt version` prints `mgtt version dev`
3. `mgtt init` scaffolds a blank `system.model.yaml`
4. `mgtt model validate examples/storefront/system.model.yaml` shows 4 components valid, 0 errors
5. `mgtt model validate testdata/models/missing-dep.yaml` shows error with suggestion
6. `mgtt model validate testdata/models/circular.yaml` shows cycle error
7. `mgtt provider install kubernetes aws` shows both providers with version and auth info
8. `mgtt provider ls` lists kubernetes and aws
9. `mgtt provider inspect kubernetes deployment` shows facts, states (degraded before starting), healthy, failure modes
10. `mgtt stdlib ls` shows all 10 primitives with base, unit, range
11. `mgtt stdlib inspect duration` shows full definition
12. Golden tests for `model validate` and `stdlib ls` pass
13. CI workflow file exists at `.github/workflows/ci.yaml`

---

## What's Next

**Plan 2** (Phases 3–4): Expression parser, state derivation, constraint engine, simulation runner. Builds on the types and loaders from this plan. The `HealthyRaw`/`WhenRaw`/`WhileRaw` string fields get compiled to `expr.Node` ASTs. The four storefront simulation scenarios become the acceptance gate.
