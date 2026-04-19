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

// TestRender_CycleTolerated — a dependency cycle produces a warning
// comment inside the mermaid block but does not error.
func TestRender_CycleTolerated(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "cyclic", Version: "1.0", Providers: []string{"k8s"}},
		Components: map[string]*model.Component{
			"a": {Name: "a", Type: "deployment", Depends: []model.Dependency{{On: []string{"b"}}}},
			"b": {Name: "b", Type: "deployment", Depends: []model.Dependency{{On: []string{"a"}}}},
		},
		Order: []string{"a", "b"},
	}
	m.BuildGraph()
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "k8s"},
		Types: map[string]*providersupport.Type{"deployment": {Name: "deployment"}},
	})
	installed := []model.InstalledProvider{{Name: "k8s"}}

	got, err := model.Render(m, reg, installed)
	if err != nil {
		t.Fatalf("cycle should not error; got %v", err)
	}
	if !strings.Contains(got, "%% warning: cycle detected") {
		t.Errorf("want cycle warning comment; got:\n%s", got)
	}
}

// TestRender_MermaidSafeNodeIDs — component keys often contain `/`, `-`,
// `.`, or `@` in real models (SSM parameter paths, k8s resource names,
// FQN-style keys). Mermaid's grammar tokenises node ids as [A-Za-z0-9_],
// so the emitted id must be sanitised. The readable original name stays
// in the quoted label.
func TestRender_MermaidSafeNodeIDs(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "ids", Version: "1.0", Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"/dev-automation/defaults/env_php": {
				Name: "/dev-automation/defaults/env_php",
				Type: "ssm_parameter",
			},
			"magento-nginx-blue": {
				Name:    "magento-nginx-blue",
				Type:    "deployment",
				Depends: []model.Dependency{{On: []string{"/dev-automation/defaults/env_php"}}},
			},
		},
		Order: []string{"magento-nginx-blue", "/dev-automation/defaults/env_php"},
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{
			"ssm_parameter": {Name: "ssm_parameter"},
			"deployment":    {Name: "deployment"},
		},
	})
	installed := []model.InstalledProvider{{Name: "p"}}

	got, err := model.Render(m, reg, installed)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	// IDs: sanitized form in both node declarations and edges.
	for _, wantID := range []string{
		`_dev_automation_defaults_env_php["/dev-automation/defaults/env_php<br/>`,
		`magento_nginx_blue["magento-nginx-blue<br/>`,
		"\n  magento_nginx_blue --> _dev_automation_defaults_env_php\n",
	} {
		if !strings.Contains(got, wantID) {
			t.Errorf("missing sanitized form %q in output:\n%s", wantID, got)
		}
	}

	// Unsanitized forms must NOT appear as node ids or edge endpoints.
	// They MAY appear inside quoted labels, which is intentional — we
	// check that edge lines don't contain the raw names.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "-->") {
			for _, bad := range []string{"/dev-automation", "magento-nginx-blue -->", "--> /dev-automation"} {
				if strings.Contains(line, bad) {
					t.Errorf("edge line %q leaks unsanitized id: contains %q", line, bad)
				}
			}
		}
	}
}

// TestRender_MultiTargetDependency — Dependency.On is []string; the
// renderer must emit one edge per target, not collapse or drop.
func TestRender_MultiTargetDependency(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "fanout", Version: "1.0", Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"api":   {Name: "api", Type: "service", Depends: []model.Dependency{{On: []string{"db", "cache"}}}},
			"db":    {Name: "db", Type: "service"},
			"cache": {Name: "cache", Type: "service"},
		},
		Order: []string{"api", "db", "cache"},
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{"service": {Name: "service"}},
	})
	installed := []model.InstalledProvider{{Name: "p"}}

	got, _ := model.Render(m, reg, installed)
	for _, want := range []string{"api --> cache", "api --> db"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing edge %q in output:\n%s", want, got)
		}
	}
}

// TestRender_ResourceSubtitleWhenDiffers — when a component has a
// non-empty Resource that differs from its key, the rendered node
// label shows the resource on a third line. This helps operators
// scanning the diagram know which AWS/kubectl resource each node
// actually probes.
func TestRender_ResourceSubtitleWhenDiffers(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "r", Version: "1.0", Providers: []string{"aws"}},
		Components: map[string]*model.Component{
			"rds": {Name: "rds", Type: "rds_instance", Resource: "flowers-stage-rds"},
			"api": {Name: "api", Type: "service"}, // no Resource set
		},
		Order: []string{"rds", "api"},
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "aws"},
		Types: map[string]*providersupport.Type{
			"rds_instance": {Name: "rds_instance"},
			"service":      {Name: "service"},
		},
	})
	installed := []model.InstalledProvider{{Name: "aws"}}

	got, _ := model.Render(m, reg, installed)

	// rds: Resource differs, show subtitle.
	if !strings.Contains(got, "rds<br/>rds_instance<br/>→ flowers-stage-rds") {
		t.Errorf("rds label missing resource subtitle; got:\n%s", got)
	}
	// api: Resource empty, no subtitle leak.
	if strings.Contains(got, "api<br/>service<br/>→") {
		t.Errorf("api shouldn't have resource subtitle; got:\n%s", got)
	}
}

// TestRender_EmptyModel — a model with 0 components renders a
// placeholder comment, not malformed mermaid.
func TestRender_EmptyModel(t *testing.T) {
	m := &model.Model{
		Meta:       model.Meta{Name: "empty", Version: "1.0"},
		Components: map[string]*model.Component{},
		Order:      []string{},
	}
	reg := providersupport.NewRegistry()
	installed := []model.InstalledProvider{}

	got, err := model.Render(m, reg, installed)
	if err != nil {
		t.Fatalf("empty model should not error; got %v", err)
	}
	if !strings.Contains(got, "%% no components") {
		t.Errorf("want 'no components' comment; got:\n%s", got)
	}
	// No edges, no subgraphs.
	if strings.Contains(got, "-->") {
		t.Errorf("empty model shouldn't have edges; got:\n%s", got)
	}
}
