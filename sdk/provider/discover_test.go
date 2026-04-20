package provider

import (
	"encoding/json"
	"testing"
)

// DiscoveryResult must round-trip through JSON unchanged — it's the
// wire format between the provider binary and mgtt-core.
func TestDiscoveryResult_JSONRoundTrip(t *testing.T) {
	orig := DiscoveryResult{
		Components: []DiscoveredComponent{
			{Name: "api", Type: "deployment", HealthFacts: []string{"ready_replicas"}, Metadata: map[string]string{"owner": "team-backend"}},
			{Name: "rds", Type: "rds_instance", HealthFacts: []string{"available"}},
		},
		Dependencies: []DiscoveredDependency{
			{From: "api", To: "rds"},
		},
	}
	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back DiscoveryResult
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Components) != 2 || back.Components[0].Name != "api" {
		t.Errorf("components corrupted: %+v", back.Components)
	}
	if back.Components[0].Metadata["owner"] != "team-backend" {
		t.Errorf("metadata lost: %+v", back.Components[0].Metadata)
	}
	if len(back.Dependencies) != 1 || back.Dependencies[0].From != "api" || back.Dependencies[0].To != "rds" {
		t.Errorf("dependencies corrupted: %+v", back.Dependencies)
	}
}
