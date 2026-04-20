package build

import (
	"testing"

	"github.com/mgt-tool/mgtt/sdk/provider"
)

func TestBuildModel_SingleProvider(t *testing.T) {
	snapshots := map[string]provider.DiscoveryResult{
		"kubernetes": {
			Components: []provider.DiscoveredComponent{
				{Name: "api", Type: "deployment"},
				{Name: "nginx", Type: "ingress"},
			},
			Dependencies: []provider.DiscoveredDependency{
				{From: "nginx", To: "api"},
			},
		},
	}
	m, err := BuildModel(snapshots)
	if err != nil {
		t.Fatalf("BuildModel: %v", err)
	}
	if len(m.Components) != 2 {
		t.Errorf("want 2 components, got %d", len(m.Components))
	}
	api, ok := m.Components["api"]
	if !ok {
		t.Fatal("api missing")
	}
	if api.Type != "deployment" {
		t.Errorf("api.Type = %q, want deployment", api.Type)
	}
	nginx := m.Components["nginx"]
	if len(nginx.Depends) != 1 || len(nginx.Depends[0].On) != 1 || nginx.Depends[0].On[0] != "api" {
		t.Errorf("nginx deps wrong: %+v", nginx.Depends)
	}
}

func TestBuildModel_MultiProvider(t *testing.T) {
	snapshots := map[string]provider.DiscoveryResult{
		"kubernetes": {
			Components: []provider.DiscoveredComponent{
				{Name: "api", Type: "deployment"},
			},
		},
		"aws": {
			Components: []provider.DiscoveredComponent{
				{Name: "rds", Type: "rds_instance"},
			},
		},
	}
	m, err := BuildModel(snapshots)
	if err != nil {
		t.Fatalf("BuildModel: %v", err)
	}
	if len(m.Components) != 2 {
		t.Errorf("want 2 components, got %d", len(m.Components))
	}
	rds := m.Components["rds"]
	if len(rds.Providers) != 1 || rds.Providers[0] != "aws" {
		t.Errorf("rds.Providers = %v, want [aws]", rds.Providers)
	}
	api := m.Components["api"]
	if len(api.Providers) != 1 || api.Providers[0] != "kubernetes" {
		t.Errorf("api.Providers = %v, want [kubernetes]", api.Providers)
	}
}

func TestBuildModel_NameCollision(t *testing.T) {
	snapshots := map[string]provider.DiscoveryResult{
		"kubernetes": {Components: []provider.DiscoveredComponent{{Name: "api", Type: "deployment"}}},
		"aws":        {Components: []provider.DiscoveredComponent{{Name: "api", Type: "rds_instance"}}},
	}
	_, err := BuildModel(snapshots)
	if err == nil {
		t.Error("colliding names across providers must error")
	}
}
