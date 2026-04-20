package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// A Registry with no discover function registered must report it
// wasn't configured — mgtt-core treats this as "provider doesn't
// support discovery, skip".
func TestRegistry_DiscoverNotRegistered(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Discover()
	if ok {
		t.Error("fresh registry must report discover not registered")
	}
}

// When a provider registers a discover fn, Registry.Discover() invokes
// it and returns the result.
func TestRegistry_DiscoverRegistered(t *testing.T) {
	r := NewRegistry()
	r.RegisterDiscover(func() (DiscoveryResult, error) {
		return DiscoveryResult{
			Components: []DiscoveredComponent{{Name: "api", Type: "deployment"}},
		}, nil
	})
	fn, ok := r.Discover()
	if !ok {
		t.Fatal("registered discover must be reported")
	}
	res, err := fn()
	if err != nil {
		t.Fatalf("fn err: %v", err)
	}
	if len(res.Components) != 1 || res.Components[0].Name != "api" {
		t.Errorf("unexpected result: %+v", res)
	}
}

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

// Run dispatching to `discover` must invoke the registered function
// and emit JSON on stdout. Exit code 0.
func TestRun_DiscoverEmitsJSON(t *testing.T) {
	r := NewRegistry()
	r.RegisterDiscover(func() (DiscoveryResult, error) {
		return DiscoveryResult{
			Components:   []DiscoveredComponent{{Name: "api", Type: "deployment"}},
			Dependencies: []DiscoveredDependency{{From: "api", To: "rds"}},
		}, nil
	})
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), r, []string{"discover"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"name":"api"`) {
		t.Errorf("stdout missing component: %s", out)
	}
	if !strings.Contains(out, `"from":"api"`) {
		t.Errorf("stdout missing dependency: %s", out)
	}
}

// If no discover function is registered, the subcommand exits with
// usage error (exit 1) — mgtt-core treats this as "skip this
// provider" and continues. Stderr carries an explanation.
func TestRun_DiscoverNotRegistered(t *testing.T) {
	r := NewRegistry()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), r, []string{"discover"}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("expected exit 1 (usage); got %d", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty; got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "discover") {
		t.Errorf("stderr should explain; got %q", stderr.String())
	}
}

// A discover function returning an error → exit 1 (usage). Error
// message on stderr. Same exit convention as the probe's unknown-type
// error since discovery failures are also caller-actionable.
func TestRun_DiscoverError(t *testing.T) {
	r := NewRegistry()
	r.RegisterDiscover(func() (DiscoveryResult, error) {
		return DiscoveryResult{}, fmt.Errorf("backend API timeout")
	})
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), r, []string{"discover"}, &stdout, &stderr)
	if code == 0 {
		t.Error("discover error must not exit 0")
	}
	if !strings.Contains(stderr.String(), "backend API timeout") {
		t.Errorf("stderr should carry the error; got %q", stderr.String())
	}
}
