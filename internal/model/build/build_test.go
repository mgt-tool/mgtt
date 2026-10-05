// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

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

// A name two providers both return is keyed by kind in each, and each
// provider's dependencies find its own component; probes read the name back.
func TestBuildModel_NameCollision(t *testing.T) {
	snapshots := map[string]provider.DiscoveryResult{
		"kubernetes": {
			Components:   []provider.DiscoveredComponent{{Name: "api", Type: "deployment"}, {Name: "edge", Type: "ingress"}},
			Dependencies: []provider.DiscoveredDependency{{From: "edge", To: "api"}},
		},
		"aws": {Components: []provider.DiscoveredComponent{{Name: "api", Type: "rds_instance"}}},
	}
	m, err := BuildModel(snapshots)
	if err != nil {
		t.Fatal(err)
	}
	dep, rds := m.Components["deployment/api"], m.Components["rds_instance/api"]
	if dep == nil || rds == nil || m.Components["api"] != nil {
		t.Fatalf("want deployment/api and rds_instance/api; got %v", m.Components)
	}
	if dep.ResourceName() != "api" || rds.ResourceName() != "api" {
		t.Errorf("probes read %q and %q; want api for both", dep.ResourceName(), rds.ResourceName())
	}
	if on := m.Components["edge"].Depends; len(on) != 1 || on[0].On[0] != "deployment/api" {
		t.Errorf("edge depends on %+v; want deployment/api", on)
	}

	// The same name and type from two providers still collides, and one
	// provider returning a name twice is an error.
	if _, err := BuildModel(map[string]provider.DiscoveryResult{
		"a": {Components: []provider.DiscoveredComponent{{Name: "api", Type: "deployment"}}},
		"b": {Components: []provider.DiscoveredComponent{{Name: "api", Type: "deployment"}}},
	}); err == nil {
		t.Error("same name and type from two providers must error")
	}
	if _, err := BuildModel(map[string]provider.DiscoveryResult{
		"a": {Components: []provider.DiscoveredComponent{{Name: "api", Type: "deployment"}, {Name: "api", Type: "service"}}},
	}); err == nil {
		t.Error("one provider returning a name twice must error")
	}
}
