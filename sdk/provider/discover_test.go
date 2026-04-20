package provider

import (
	"bytes"
	"encoding/json"
	"reflect"
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
	if !reflect.DeepEqual(orig, back) {
		t.Errorf("round-trip mismatch\norig: %+v\nback: %+v", orig, back)
	}

	// Verify that omitempty actually omits empty optional fields on the wire.
	minimal, err := json.Marshal(DiscoveredComponent{Name: "x", Type: "y"})
	if err != nil {
		t.Fatalf("marshal minimal: %v", err)
	}
	for _, omitted := range []string{`"health_facts":`, `"metadata":`} {
		if bytes.Contains(minimal, []byte(omitted)) {
			t.Errorf("omitempty failed: %s present in %s", omitted, minimal)
		}
	}
}
