package providersupport

import (
	"os"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Stdlib tests
// ---------------------------------------------------------------------------

func TestStdlib_HasAllPrimitives(t *testing.T) {
	expected := map[string]string{
		"int":        "int",
		"float":      "float",
		"bool":       "bool",
		"string":     "string",
		"duration":   "float",
		"bytes":      "int",
		"ratio":      "float",
		"percentage": "float",
		"count":      "int",
		"timestamp":  "string",
	}

	if len(Stdlib) != len(expected) {
		t.Errorf("Stdlib has %d entries, want %d", len(Stdlib), len(expected))
	}

	for name, wantBase := range expected {
		dt, ok := Stdlib[name]
		if !ok {
			t.Errorf("Stdlib missing %q", name)
			continue
		}
		if dt.Name != name {
			t.Errorf("Stdlib[%q].Name = %q, want %q", name, dt.Name, name)
		}
		if dt.Base != wantBase {
			t.Errorf("Stdlib[%q].Base = %q, want %q", name, dt.Base, wantBase)
		}
	}
}

func TestStdlib_DurationHasUnits(t *testing.T) {
	d, ok := Stdlib["duration"]
	if !ok {
		t.Fatal("Stdlib missing duration")
	}

	wantUnits := []string{"ms", "s", "m", "h", "d"}
	if len(d.Units) != len(wantUnits) {
		t.Errorf("duration units = %v, want %v", d.Units, wantUnits)
	}
	for i, u := range wantUnits {
		if i >= len(d.Units) || d.Units[i] != u {
			t.Errorf("duration.Units[%d] = %q, want %q", i, d.Units[i], u)
		}
	}

	if d.Range == nil {
		t.Fatal("duration.Range is nil")
	}
	if d.Range.Min == nil {
		t.Fatal("duration.Range.Min is nil")
	}
	if *d.Range.Min != 0.0 {
		t.Errorf("duration.Range.Min = %v, want 0.0", *d.Range.Min)
	}
	if d.Range.Max != nil {
		t.Errorf("duration.Range.Max should be nil, got %v", *d.Range.Max)
	}
}

func TestStdlib_RatioRange(t *testing.T) {
	r, ok := Stdlib["ratio"]
	if !ok {
		t.Fatal("Stdlib missing ratio")
	}
	if r.Range == nil {
		t.Fatal("ratio.Range is nil")
	}
	if r.Range.Min == nil || *r.Range.Min != 0.0 {
		t.Errorf("ratio.Range.Min = %v, want 0.0", r.Range.Min)
	}
	if r.Range.Max == nil || *r.Range.Max != 1.0 {
		t.Errorf("ratio.Range.Max = %v, want 1.0", r.Range.Max)
	}
}

// ---------------------------------------------------------------------------
// Load tests
// ---------------------------------------------------------------------------

const minimalYAML = `
meta:
  name: testprovider
  version: 0.1.0
  description: minimal test provider

types:
  mytype:
    facts:
      cpu_usage:
        type: mgtt.percentage
        ttl: 10s
        probe:
          cmd: "cat /proc/stat"
          parse: float
          cost: low
    healthy: ["cpu_usage < 90"]
    states:
      ok:
        when: "cpu_usage < 90"
        description: normal load
      overloaded:
        when: "cpu_usage >= 90"
        description: high cpu
    default_active_state: ok
    failure_modes:
      overloaded:
        can_cause: [slowness, timeout]
`

func TestLoadProvider_Minimal(t *testing.T) {
	p, err := LoadFromBytes([]byte(minimalYAML))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}

	if p.Meta.Name != "testprovider" {
		t.Errorf("Meta.Name = %q, want testprovider", p.Meta.Name)
	}
	if p.Meta.Version != "0.1.0" {
		t.Errorf("Meta.Version = %q, want 0.1.0", p.Meta.Version)
	}

	mt, ok := p.Types["mytype"]
	if !ok {
		t.Fatal("missing type mytype")
	}

	if len(mt.Facts) != 1 {
		t.Errorf("mytype facts count = %d, want 1", len(mt.Facts))
	}
	fs, ok := mt.Facts["cpu_usage"]
	if !ok {
		t.Fatal("missing fact cpu_usage")
	}
	if fs.TypeName != "mgtt.percentage" {
		t.Errorf("cpu_usage.TypeName = %q, want mgtt.percentage", fs.TypeName)
	}
	if fs.TTL != 10*time.Second {
		t.Errorf("cpu_usage.TTL = %v, want 10s", fs.TTL)
	}
	if fs.Probe.Cost != "low" {
		t.Errorf("cpu_usage.Probe.Cost = %q, want low", fs.Probe.Cost)
	}

	if len(mt.HealthyRaw) != 1 || mt.HealthyRaw[0] != "cpu_usage < 90" {
		t.Errorf("HealthyRaw = %v, want [cpu_usage < 90]", mt.HealthyRaw)
	}

	if mt.DefaultActiveState != "ok" {
		t.Errorf("DefaultActiveState = %q, want ok", mt.DefaultActiveState)
	}

	causes := mt.FailureModes["overloaded"]
	if len(causes) != 2 || causes[0] != "slowness" || causes[1] != "timeout" {
		t.Errorf("FailureModes[overloaded] = %v, want [slowness timeout]", causes)
	}
}

func TestLoadProvider_StateOrder(t *testing.T) {
	p, err := LoadFromBytes([]byte(minimalYAML))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}

	mt := p.Types["mytype"]
	if len(mt.States) != 2 {
		t.Fatalf("states count = %d, want 2", len(mt.States))
	}
	if mt.States[0].Name != "ok" {
		t.Errorf("States[0].Name = %q, want ok", mt.States[0].Name)
	}
	if mt.States[1].Name != "overloaded" {
		t.Errorf("States[1].Name = %q, want overloaded", mt.States[1].Name)
	}
}

func TestLoadFromFile_Kubernetes(t *testing.T) {
	p, err := LoadFromFile("../../testdata/providers/compute.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile compute: %v", err)
	}

	if p.Meta.Name != "compute" {
		t.Errorf("Meta.Name = %q, want compute", p.Meta.Name)
	}

	// Check gateway type exists.
	gateway, ok := p.Types["gateway"]
	if !ok {
		t.Fatal("missing type gateway")
	}
	if _, ok := gateway.Facts["upstream_count"]; !ok {
		t.Error("gateway missing fact upstream_count")
	}

	// Check workload type exists.
	deploy, ok := p.Types["workload"]
	if !ok {
		t.Fatal("missing type workload")
	}

	// Check 4 workload states in correct order.
	wantOrder := []string{"degraded", "draining", "starting", "live"}
	if len(deploy.States) != len(wantOrder) {
		t.Fatalf("workload states count = %d, want %d", len(deploy.States), len(wantOrder))
	}
	for i, want := range wantOrder {
		if deploy.States[i].Name != want {
			t.Errorf("workload.States[%d].Name = %q, want %q", i, deploy.States[i].Name, want)
		}
	}

	// Verify degraded is before starting (critical design constraint).
	degradedIdx := -1
	startingIdx := -1
	for i, s := range deploy.States {
		switch s.Name {
		case "degraded":
			degradedIdx = i
		case "starting":
			startingIdx = i
		}
	}
	if degradedIdx >= startingIdx {
		t.Errorf("degraded (idx %d) must come before starting (idx %d)", degradedIdx, startingIdx)
	}

	// Verify workload has 4 facts.
	wantFacts := []string{"ready_replicas", "desired_replicas", "restart_count", "endpoints"}
	for _, fn := range wantFacts {
		if _, ok := deploy.Facts[fn]; !ok {
			t.Errorf("workload missing fact %q", fn)
		}
	}

	// Check TTL is parsed correctly.
	if deploy.Facts["ready_replicas"].TTL != 30*time.Second {
		t.Errorf("ready_replicas.TTL = %v, want 30s", deploy.Facts["ready_replicas"].TTL)
	}
}

func TestLoadFromFile_AWS(t *testing.T) {
	p, err := LoadFromFile("../../testdata/providers/datalayer.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile datalayer: %v", err)
	}

	if p.Meta.Name != "datalayer" {
		t.Errorf("Meta.Name = %q, want datalayer", p.Meta.Name)
	}

	store, ok := p.Types["datastore"]
	if !ok {
		t.Fatal("missing type datastore")
	}

	if _, ok := store.Facts["available"]; !ok {
		t.Error("datastore missing fact available")
	}
	if _, ok := store.Facts["connection_count"]; !ok {
		t.Error("datastore missing fact connection_count")
	}

	if store.DefaultActiveState != "live" {
		t.Errorf("datastore.DefaultActiveState = %q, want live", store.DefaultActiveState)
	}

	if len(store.States) != 2 {
		t.Errorf("datastore.States count = %d, want 2", len(store.States))
	}

	causes := store.FailureModes["stopped"]
	if len(causes) != 3 {
		t.Errorf("datastore FailureModes[stopped] = %v, want 3 entries", causes)
	}

	if store.Facts["available"].TTL != 60*time.Second {
		t.Errorf("available.TTL = %v, want 60s", store.Facts["available"].TTL)
	}
}

// ---------------------------------------------------------------------------
// Registry tests
// ---------------------------------------------------------------------------

func loadTestProviders(t *testing.T) (*Registry, *Provider, *Provider) {
	t.Helper()
	k8s, err := LoadFromFile("../../testdata/providers/compute.yaml")
	if err != nil {
		t.Fatalf("load compute: %v", err)
	}
	datalayer, err := LoadFromFile("../../testdata/providers/datalayer.yaml")
	if err != nil {
		t.Fatalf("load datalayer: %v", err)
	}
	reg := NewRegistry()
	reg.Register(k8s)
	reg.Register(datalayer)
	return reg, k8s, datalayer
}

func TestRegistry_ResolveType(t *testing.T) {
	reg, _, _ := loadTestProviders(t)

	// Resolve workload → should come from compute.
	typ, provName, err := reg.ResolveType([]string{"compute", "datalayer"}, "workload")
	if err != nil {
		t.Fatalf("ResolveType workload: %v", err)
	}
	if provName != "compute" {
		t.Errorf("provider = %q, want compute", provName)
	}
	if typ.Name != "workload" {
		t.Errorf("type.Name = %q, want workload", typ.Name)
	}

	// Resolve datastore → should come from datalayer.
	typ, provName, err = reg.ResolveType([]string{"compute", "datalayer"}, "datastore")
	if err != nil {
		t.Fatalf("ResolveType datastore: %v", err)
	}
	if provName != "datalayer" {
		t.Errorf("provider = %q, want datalayer", provName)
	}
	if typ.Name != "datastore" {
		t.Errorf("type.Name = %q, want datastore", typ.Name)
	}
}

func TestRegistry_ResolveType_NotFound(t *testing.T) {
	reg, _, _ := loadTestProviders(t)

	_, _, err := reg.ResolveType([]string{"compute", "datalayer"}, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
}

func TestRegistry_PeckingOrder(t *testing.T) {
	// Both providers declare "gateway" — first one wins.
	// We'll use a synthetic provider that also declares "gateway".
	const secondProvYAML = `
meta:
  name: second
  version: 0.1.0
types:
  gateway:
    facts:
      upstream_count:
        type: mgtt.int
        ttl: 30s
    states:
      live:
        when: "upstream_count > 0"
        description: always live
    default_active_state: live
`
	second, err := LoadFromBytes([]byte(secondProvYAML))
	if err != nil {
		t.Fatalf("load second: %v", err)
	}

	k8s, err := LoadFromFile("../../testdata/providers/compute.yaml")
	if err != nil {
		t.Fatalf("load compute: %v", err)
	}

	reg := NewRegistry()
	reg.Register(k8s)    // registered first — higher priority
	reg.Register(second) // registered second — lower priority

	// compute is listed first in componentProviders → wins.
	_, provName, err := reg.ResolveType([]string{"compute", "second"}, "gateway")
	if err != nil {
		t.Fatalf("ResolveType: %v", err)
	}
	if provName != "compute" {
		t.Errorf("provider = %q, want compute (pecking order)", provName)
	}

	// second is listed first → wins.
	_, provName, err = reg.ResolveType([]string{"second", "compute"}, "gateway")
	if err != nil {
		t.Fatalf("ResolveType: %v", err)
	}
	if provName != "second" {
		t.Errorf("provider = %q, want second (pecking order)", provName)
	}
}

func TestRegistry_ExplicitNamespace(t *testing.T) {
	// Even if compute is listed first, explicit "datalayer.datastore" bypasses scan.
	reg, _, _ := loadTestProviders(t)

	typ, provName, err := reg.ResolveType([]string{"compute"}, "datalayer.datastore")
	if err != nil {
		t.Fatalf("ResolveType datalayer.datastore: %v", err)
	}
	if provName != "datalayer" {
		t.Errorf("provider = %q, want datalayer", provName)
	}
	if typ.Name != "datastore" {
		t.Errorf("type.Name = %q, want datastore", typ.Name)
	}
}

func TestLoadProvider_CompiledExpressions(t *testing.T) {
	k8s, err := LoadFromFile("../../testdata/providers/compute.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile compute: %v", err)
	}

	deploy, ok := k8s.Types["workload"]
	if !ok {
		t.Fatal("missing type workload")
	}

	// Verify workload.Healthy has 3 compiled nodes (non-nil).
	if len(deploy.Healthy) != 3 {
		t.Errorf("workload.Healthy compiled nodes = %d, want 3", len(deploy.Healthy))
	}
	for i, node := range deploy.Healthy {
		if node == nil {
			t.Errorf("workload.Healthy[%d] is nil", i)
		}
	}

	// Verify workload.States[0] is "degraded" and has non-nil When.
	if len(deploy.States) == 0 {
		t.Fatal("workload has no states")
	}
	if deploy.States[0].Name != "degraded" {
		t.Errorf("workload.States[0].Name = %q, want degraded", deploy.States[0].Name)
	}
	if deploy.States[0].When == nil {
		t.Error("workload.States[0].When (degraded) is nil, want compiled node")
	}

	// Verify all states with WhenRaw have compiled When nodes.
	for _, sd := range deploy.States {
		if sd.WhenRaw != "" && sd.When == nil {
			t.Errorf("state %q has WhenRaw %q but When is nil", sd.Name, sd.WhenRaw)
		}
	}

	// Verify HealthyRaw is still present (for display).
	if len(deploy.HealthyRaw) != 3 {
		t.Errorf("workload.HealthyRaw = %d, want 3 (raw strings must be kept)", len(deploy.HealthyRaw))
	}
}

func TestRegistry_GetAndAll(t *testing.T) {
	reg, k8s, datalayer := loadTestProviders(t)

	if p, ok := reg.Get("compute"); !ok || p != k8s {
		t.Error("Get(compute) did not return the registered provider")
	}
	if p, ok := reg.Get("datalayer"); !ok || p != datalayer {
		t.Error("Get(datalayer) did not return the registered provider")
	}

	all := reg.All()
	if len(all) != 2 || all[0] != k8s || all[1] != datalayer {
		t.Errorf("All() = %v, want [k8s, datalayer] in registration order", all)
	}
}

func TestLoadFromDir_MultiFile(t *testing.T) {
	p, err := LoadFromDir("testdata/multifile")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	if p.Meta.Name != "multitest" {
		t.Errorf("Meta.Name = %q, want multitest", p.Meta.Name)
	}

	if p.Variables["namespace"].Default != "default" {
		t.Errorf("namespace default = %q, want default", p.Variables["namespace"].Default)
	}

	mt, ok := p.Types["mytype"]
	if !ok {
		t.Fatal("missing type mytype")
	}

	if _, ok := mt.Facts["ready"]; !ok {
		t.Error("mytype missing fact ready")
	}

	if mt.DefaultActiveState != "live" {
		t.Errorf("DefaultActiveState = %q, want live", mt.DefaultActiveState)
	}

	if len(mt.States) != 2 {
		t.Fatalf("states count = %d, want 2", len(mt.States))
	}
	if mt.States[0].Name != "missing" {
		t.Errorf("States[0].Name = %q, want missing", mt.States[0].Name)
	}

	// Verify expressions are compiled.
	if len(mt.Healthy) != 1 || mt.Healthy[0] == nil {
		t.Error("healthy expression not compiled")
	}
	if mt.States[0].When == nil {
		t.Error("state missing.When not compiled")
	}

	causes := mt.FailureModes["missing"]
	if len(causes) != 1 || causes[0] != "upstream_failure" {
		t.Errorf("FailureModes[missing] = %v, want [upstream_failure]", causes)
	}
}

func TestLoadFromDir_FallsBackToInlineTypes(t *testing.T) {
	dir := t.TempDir()
	data, err := os.ReadFile("../../testdata/providers/compute.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/provider.yaml", data, 0644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir with inline types: %v", err)
	}

	if p.Meta.Name != "compute" {
		t.Errorf("Meta.Name = %q, want compute", p.Meta.Name)
	}
	if _, ok := p.Types["workload"]; !ok {
		t.Fatal("missing type workload — inline types not loaded")
	}
}
