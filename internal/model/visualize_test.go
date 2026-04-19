package model_test

import (
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// TestRender_MinimalTwoNode covers the happy path: a two-component model
// with one dependency renders an H1 header, a mermaid block opener, one
// subgraph (or flat since both components share a provider), node
// declarations, and an edge.
func TestRender_MinimalTwoNode(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "demo", Version: "1.0", Providers: []string{"k8s"}},
		Components: map[string]*model.Component{
			"frontend": {Name: "frontend", Type: "deployment", Depends: []model.Dependency{{On: []string{"backend"}}}},
			"backend":  {Name: "backend", Type: "deployment"},
		},
		Order: []string{"frontend", "backend"},
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "k8s"},
		Types: map[string]*providersupport.Type{"deployment": {Name: "deployment"}},
	})
	installed := []model.InstalledProvider{
		{Name: "k8s", Namespace: "", Version: "1.0.0"},
	}

	got, err := model.Render(m, reg, installed)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	for _, want := range []string{
		"# demo — dependency graph",
		"```mermaid",
		"graph LR",
		`frontend["frontend<br/>deployment"]`,
		`backend["backend<br/>deployment"]`,
		"frontend --> backend",
		"\n```\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

// TestRender_ShapesByTypeClass — each pattern group gets its expected
// bracket syntax in the emitted mermaid.
func TestRender_ShapesByTypeClass(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "shapes", Version: "1.0", Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"db":     {Name: "db", Type: "rds_instance"},
			"cache":  {Name: "cache", Type: "elasticache_cluster"},
			"bucket": {Name: "bucket", Type: "s3_bucket"},
			"queue":  {Name: "queue", Type: "mq_broker"},
			"edge":   {Name: "edge", Type: "ingress"},
			"cdn":    {Name: "cdn", Type: "cdn"},
			"pod":    {Name: "pod", Type: "deployment"},
		},
		Order: []string{"db", "cache", "bucket", "queue", "edge", "cdn", "pod"},
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{
			"rds_instance":        {Name: "rds_instance"},
			"elasticache_cluster": {Name: "elasticache_cluster"},
			"s3_bucket":           {Name: "s3_bucket"},
			"mq_broker":           {Name: "mq_broker"},
			"ingress":             {Name: "ingress"},
			"cdn":                 {Name: "cdn"},
			"deployment":          {Name: "deployment"},
		},
	})
	installed := []model.InstalledProvider{{Name: "p"}}

	got, _ := model.Render(m, reg, installed)

	for _, tc := range []struct {
		name, want, desc string
	}{
		{"db", `db[("db<br/>rds_instance")]`, "cylinder"},
		{"cache", `cache[("cache<br/>elasticache_cluster")]`, "cylinder"},
		{"bucket", `bucket[("bucket<br/>s3_bucket")]`, "cylinder"},
		{"queue", `queue[/"queue<br/>mq_broker"\]`, "trapezoid"},
		{"edge", `edge(["edge<br/>ingress"])`, "rounded"},
		{"cdn", `cdn(["cdn<br/>cdn"])`, "rounded"},
		{"pod", `pod["pod<br/>deployment"]`, "rectangle"},
	} {
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s (%s): missing %q in output:\n%s", tc.name, tc.desc, tc.want, got)
		}
	}
}

// TestRender_MultiProviderSubgraphs — components are grouped into one
// mermaid subgraph per resolved provider FQN. Subgraphs sorted
// alphabetically by FQN; the synthetic "generic" bucket sorts last
// (no components fall back here in this test).
func TestRender_MultiProviderSubgraphs(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "multi", Version: "1.0", Providers: []string{"k8s", "aws"}},
		Components: map[string]*model.Component{
			"nginx": {Name: "nginx", Type: "deployment", Depends: []model.Dependency{{On: []string{"rds"}}}},
			"rds":   {Name: "rds", Type: "rds_instance"},
		},
		Order: []string{"nginx", "rds"},
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "k8s"},
		Types: map[string]*providersupport.Type{"deployment": {Name: "deployment"}},
	})
	reg.Register(&providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "aws"},
		Types: map[string]*providersupport.Type{"rds_instance": {Name: "rds_instance"}},
	})
	installed := []model.InstalledProvider{
		{Name: "k8s", Namespace: "mgt-tool", Version: "3.0.0"},
		{Name: "aws", Namespace: "mgt-tool", Version: "1.0.0"},
	}

	got, err := model.Render(m, reg, installed)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	// Two subgraphs, sorted by FQN.
	awsIdx := strings.Index(got, `subgraph mgt_tool_aws ["mgt-tool/aws"]`)
	k8sIdx := strings.Index(got, `subgraph mgt_tool_k8s ["mgt-tool/k8s"]`)
	if awsIdx < 0 || k8sIdx < 0 {
		t.Fatalf("missing subgraph declarations in:\n%s", got)
	}
	if awsIdx > k8sIdx {
		t.Errorf("aws subgraph should appear before k8s (alphabetical FQN); got aws@%d k8s@%d", awsIdx, k8sIdx)
	}
	// Each component inside its provider's subgraph.
	if !strings.Contains(got, "subgraph mgt_tool_aws") || !strings.Contains(got[awsIdx:], `rds[(`) {
		t.Errorf("rds should be inside mgt-tool/aws subgraph:\n%s", got)
	}
	// Edges emitted at the top level after all subgraphs close.
	if !strings.Contains(got, "nginx --> rds") {
		t.Errorf("missing cross-subgraph edge; got:\n%s", got)
	}
	// Subgraphs are closed with `end`.
	if strings.Count(got, "\n  end\n") != 2 {
		t.Errorf("expected 2 subgraph closers, got %d:\n%s", strings.Count(got, "\n  end\n"), got)
	}
}

// TestRender_SingleProviderFlat — when all components resolve to the
// same provider, no subgraph is emitted (flat layout).
func TestRender_SingleProviderFlat(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "flat", Version: "1.0", Providers: []string{"k8s"}},
		Components: map[string]*model.Component{
			"a": {Name: "a", Type: "deployment", Depends: []model.Dependency{{On: []string{"b"}}}},
			"b": {Name: "b", Type: "deployment"},
		},
		Order: []string{"a", "b"},
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "k8s"},
		Types: map[string]*providersupport.Type{"deployment": {Name: "deployment"}},
	})
	installed := []model.InstalledProvider{{Name: "k8s", Namespace: "mgt-tool"}}

	got, _ := model.Render(m, reg, installed)

	if strings.Contains(got, "subgraph ") {
		t.Errorf("single-provider model should render flat (no subgraph); got:\n%s", got)
	}
	if !strings.Contains(got, "a --> b") {
		t.Errorf("edge missing; got:\n%s", got)
	}
}

// TestRender_Deterministic — calling Render twice with the same
// inputs must produce byte-identical output. Guards against map
// iteration order leaking into the diagram.
func TestRender_Deterministic(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "det", Version: "1.0", Providers: []string{"a", "b", "c"}},
		Components: map[string]*model.Component{
			"z": {Name: "z", Type: "service", Depends: []model.Dependency{{On: []string{"m"}}, {On: []string{"a"}}}},
			"m": {Name: "m", Type: "service", Depends: []model.Dependency{{On: []string{"a"}}}},
			"a": {Name: "a", Type: "service"},
		},
		Order: []string{"z", "m", "a"},
	}
	reg := providersupport.NewRegistry()
	for _, p := range []string{"a", "b", "c"} {
		reg.Register(&providersupport.Provider{
			Meta:  providersupport.ProviderMeta{Name: p},
			Types: map[string]*providersupport.Type{"service": {Name: "service"}},
		})
	}
	installed := []model.InstalledProvider{
		{Name: "a", Namespace: "ns"}, {Name: "b", Namespace: "ns"}, {Name: "c", Namespace: "ns"},
	}

	first, _ := model.Render(m, reg, installed)
	for i := 0; i < 20; i++ {
		got, _ := model.Render(m, reg, installed)
		if got != first {
			t.Fatalf("Render non-deterministic on iteration %d:\nfirst:\n%s\ngot:\n%s", i, first, got)
		}
	}
}
