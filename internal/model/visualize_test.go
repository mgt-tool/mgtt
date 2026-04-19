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
