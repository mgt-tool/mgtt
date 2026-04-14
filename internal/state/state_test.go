package state

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	factspkg "github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// repoRoot returns the absolute path to the repository root, anchored to
// the location of this test file.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	// file is .../internal/state/state_test.go; root is two dirs up.
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// loadStorefront loads the storefront model and both providers, returning the
// model and a populated registry ready for Derive calls.
func loadStorefront(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()

	root := repoRoot()
	m, err := model.Load(filepath.Join(root, "testdata", "models", "sample-model.yaml"))
	if err != nil {
		t.Fatalf("load storefront model: %v", err)
	}

	reg := providersupport.NewRegistry()
	for _, pair := range []struct{ name, file string }{
		{"kubernetes", "testdata/kubernetes-provider.yaml"},
		{"aws", "testdata/aws-provider.yaml"},
	} {
		p, err := providersupport.LoadFromFile(pair.file)
		if err != nil {
			t.Fatalf("load provider %s: %v", pair.name, err)
		}
		reg.Register(p)
	}
	return m, reg
}

// fact is a convenience constructor for a facts.Fact.
func fact(key string, value any) factspkg.Fact {
	return factspkg.Fact{Key: key, Value: value, At: time.Now()}
}

// newStore builds a fact store with the provided per-component facts.
// facts is a map[componentName]map[factKey]factValue.
func newStore(facts map[string]map[string]any) *factspkg.Store {
	store := factspkg.NewInMemory()
	for comp, kv := range facts {
		for k, v := range kv {
			store.Append(comp, factspkg.Fact{Key: k, Value: v, At: time.Now()})
		}
	}
	return store
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// Test 1: API degraded — ready_replicas=0, desired_replicas=3, restart_count=47
// degraded condition: ready_replicas < desired_replicas & restart_count > 5
// 0 < 3 = true, 47 > 5 = true → AND = true → state = "degraded"
func TestDerive_APIDegraded(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"api": {
			"ready_replicas":   int(0),
			"desired_replicas": int(3),
			"restart_count":    int(47),
		},
	})

	d := Derive(m, reg, store)

	if got := d.ComponentStates["api"]; got != "degraded" {
		t.Errorf("api state = %q, want %q", got, "degraded")
	}
}

// Test 2: API starting (missing restart_count) — ready_replicas=0, desired_replicas=3
//
// This is the critical test: missing restart_count causes the degraded
// condition to record an UnresolvedError and fall through to starting, which
// matches because ready_replicas < desired_replicas is true.
func TestDerive_APIStarting_MissingRestartCount(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"api": {
			"ready_replicas":   int(0),
			"desired_replicas": int(3),
			// restart_count intentionally absent
		},
	})

	d := Derive(m, reg, store)

	// State should be "starting", not "degraded" or "unknown".
	if got := d.ComponentStates["api"]; got != "starting" {
		t.Errorf("api state = %q, want %q", got, "starting")
	}

	// UnresolvedBy["api"] must have at least one entry recording that restart_count
	// was missing during the degraded state evaluation.
	unresolved := d.UnresolvedBy["api"]
	if len(unresolved) == 0 {
		t.Fatal("expected UnresolvedBy[api] to be non-empty (restart_count missing), got empty")
	}

	found := false
	for _, ue := range unresolved {
		if ue.Fact == "restart_count" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected an UnresolvedError for restart_count in UnresolvedBy[api], got: %v", unresolved)
	}
}

// Test 3: API live — ready_replicas=3, desired_replicas=3
// live condition: ready_replicas == desired_replicas → true
func TestDerive_APILive(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"api": {
			"ready_replicas":   int(3),
			"desired_replicas": int(3),
			"restart_count":    int(0),
		},
	})

	d := Derive(m, reg, store)

	if got := d.ComponentStates["api"]; got != "live" {
		t.Errorf("api state = %q, want %q", got, "live")
	}
}

// Test 4: RDS live — available=true
func TestDerive_RDSLive(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"rds": {
			"available": true,
		},
	})

	d := Derive(m, reg, store)

	if got := d.ComponentStates["rds"]; got != "live" {
		t.Errorf("rds state = %q, want %q", got, "live")
	}
}

// Test 5: RDS stopped — available=false
func TestDerive_RDSStopped(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"rds": {
			"available": false,
		},
	})

	d := Derive(m, reg, store)

	if got := d.ComponentStates["rds"]; got != "stopped" {
		t.Errorf("rds state = %q, want %q", got, "stopped")
	}
}

// Test 6: No facts — empty store → all components are "unknown"
func TestDerive_NoFacts_AllUnknown(t *testing.T) {
	m, reg := loadStorefront(t)
	store := factspkg.NewInMemory()

	d := Derive(m, reg, store)

	for _, name := range m.Order {
		if got := d.ComponentStates[name]; got != "unknown" {
			t.Errorf("component %q: state = %q, want %q", name, got, "unknown")
		}
	}
}

// Test 7: nginx draining — upstream_count=0 → state="draining"
func TestDerive_NginxDraining(t *testing.T) {
	m, reg := loadStorefront(t)
	store := newStore(map[string]map[string]any{
		"nginx": {
			"upstream_count": int(0),
		},
	})

	d := Derive(m, reg, store)

	if got := d.ComponentStates["nginx"]; got != "draining" {
		t.Errorf("nginx state = %q, want %q", got, "draining")
	}
}
