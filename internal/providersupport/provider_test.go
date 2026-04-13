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
	p, err := LoadFromFile("testdata/kubernetes.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile kubernetes: %v", err)
	}

	if p.Meta.Name != "kubernetes" {
		t.Errorf("Meta.Name = %q, want kubernetes", p.Meta.Name)
	}

	// Check ingress type exists.
	ingress, ok := p.Types["ingress"]
	if !ok {
		t.Fatal("missing type ingress")
	}
	if _, ok := ingress.Facts["upstream_count"]; !ok {
		t.Error("ingress missing fact upstream_count")
	}

	// Check deployment type exists.
	deploy, ok := p.Types["deployment"]
	if !ok {
		t.Fatal("missing type deployment")
	}

	// Check 4 deployment states in correct order.
	wantOrder := []string{"degraded", "draining", "starting", "live"}
	if len(deploy.States) != len(wantOrder) {
		t.Fatalf("deployment states count = %d, want %d", len(deploy.States), len(wantOrder))
	}
	for i, want := range wantOrder {
		if deploy.States[i].Name != want {
			t.Errorf("deployment.States[%d].Name = %q, want %q", i, deploy.States[i].Name, want)
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

	// Verify deployment has 4 facts.
	wantFacts := []string{"ready_replicas", "desired_replicas", "restart_count", "endpoints"}
	for _, fn := range wantFacts {
		if _, ok := deploy.Facts[fn]; !ok {
			t.Errorf("deployment missing fact %q", fn)
		}
	}

	// Check TTL is parsed correctly.
	if deploy.Facts["ready_replicas"].TTL != 30*time.Second {
		t.Errorf("ready_replicas.TTL = %v, want 30s", deploy.Facts["ready_replicas"].TTL)
	}
}

func TestLoadFromFile_AWS(t *testing.T) {
	p, err := LoadFromFile("testdata/aws.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile aws: %v", err)
	}

	if p.Meta.Name != "aws" {
		t.Errorf("Meta.Name = %q, want aws", p.Meta.Name)
	}

	rds, ok := p.Types["rds_instance"]
	if !ok {
		t.Fatal("missing type rds_instance")
	}

	if _, ok := rds.Facts["available"]; !ok {
		t.Error("rds_instance missing fact available")
	}
	if _, ok := rds.Facts["connection_count"]; !ok {
		t.Error("rds_instance missing fact connection_count")
	}

	if rds.DefaultActiveState != "live" {
		t.Errorf("rds_instance.DefaultActiveState = %q, want live", rds.DefaultActiveState)
	}

	if len(rds.States) != 2 {
		t.Errorf("rds_instance.States count = %d, want 2", len(rds.States))
	}

	causes := rds.FailureModes["stopped"]
	if len(causes) != 3 {
		t.Errorf("rds_instance FailureModes[stopped] = %v, want 3 entries", causes)
	}

	if rds.Facts["available"].TTL != 60*time.Second {
		t.Errorf("available.TTL = %v, want 60s", rds.Facts["available"].TTL)
	}
}

// ---------------------------------------------------------------------------
// Registry tests
// ---------------------------------------------------------------------------

func loadTestProviders(t *testing.T) (*Registry, *Provider, *Provider) {
	t.Helper()
	k8s, err := LoadFromFile("testdata/kubernetes.yaml")
	if err != nil {
		t.Fatalf("load kubernetes: %v", err)
	}
	aws, err := LoadFromFile("testdata/aws.yaml")
	if err != nil {
		t.Fatalf("load aws: %v", err)
	}
	reg := NewRegistry()
	reg.Register(k8s)
	reg.Register(aws)
	return reg, k8s, aws
}

func TestRegistry_ResolveType(t *testing.T) {
	reg, _, _ := loadTestProviders(t)

	// Resolve deployment → should come from kubernetes.
	typ, provName, err := reg.ResolveType([]string{"kubernetes", "aws"}, "deployment")
	if err != nil {
		t.Fatalf("ResolveType deployment: %v", err)
	}
	if provName != "kubernetes" {
		t.Errorf("provider = %q, want kubernetes", provName)
	}
	if typ.Name != "deployment" {
		t.Errorf("type.Name = %q, want deployment", typ.Name)
	}

	// Resolve rds_instance → should come from aws.
	typ, provName, err = reg.ResolveType([]string{"kubernetes", "aws"}, "rds_instance")
	if err != nil {
		t.Fatalf("ResolveType rds_instance: %v", err)
	}
	if provName != "aws" {
		t.Errorf("provider = %q, want aws", provName)
	}
	if typ.Name != "rds_instance" {
		t.Errorf("type.Name = %q, want rds_instance", typ.Name)
	}
}

func TestRegistry_ResolveType_NotFound(t *testing.T) {
	reg, _, _ := loadTestProviders(t)

	_, _, err := reg.ResolveType([]string{"kubernetes", "aws"}, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
}

func TestRegistry_PeckingOrder(t *testing.T) {
	// Both providers declare "ingress" — first one wins.
	// We'll use a synthetic provider that also declares "ingress".
	const secondProvYAML = `
meta:
  name: second
  version: 0.1.0
types:
  ingress:
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

	k8s, err := LoadFromFile("testdata/kubernetes.yaml")
	if err != nil {
		t.Fatalf("load kubernetes: %v", err)
	}

	reg := NewRegistry()
	reg.Register(k8s)    // registered first — higher priority
	reg.Register(second) // registered second — lower priority

	// kubernetes is listed first in componentProviders → wins.
	_, provName, err := reg.ResolveType([]string{"kubernetes", "second"}, "ingress")
	if err != nil {
		t.Fatalf("ResolveType: %v", err)
	}
	if provName != "kubernetes" {
		t.Errorf("provider = %q, want kubernetes (pecking order)", provName)
	}

	// second is listed first → wins.
	_, provName, err = reg.ResolveType([]string{"second", "kubernetes"}, "ingress")
	if err != nil {
		t.Fatalf("ResolveType: %v", err)
	}
	if provName != "second" {
		t.Errorf("provider = %q, want second (pecking order)", provName)
	}
}

func TestRegistry_ExplicitNamespace(t *testing.T) {
	// Even if kubernetes is listed first, explicit "aws.rds_instance" bypasses scan.
	reg, _, _ := loadTestProviders(t)

	typ, provName, err := reg.ResolveType([]string{"kubernetes"}, "aws.rds_instance")
	if err != nil {
		t.Fatalf("ResolveType aws.rds_instance: %v", err)
	}
	if provName != "aws" {
		t.Errorf("provider = %q, want aws", provName)
	}
	if typ.Name != "rds_instance" {
		t.Errorf("type.Name = %q, want rds_instance", typ.Name)
	}
}

func TestRegistry_QueryMethods(t *testing.T) {
	reg, _, _ := loadTestProviders(t)

	// DefaultActiveStateFor
	das, err := reg.DefaultActiveStateFor("kubernetes", "deployment")
	if err != nil {
		t.Fatalf("DefaultActiveStateFor: %v", err)
	}
	if das != "live" {
		t.Errorf("DefaultActiveStateFor = %q, want live", das)
	}

	// FailureModesFor
	causes, err := reg.FailureModesFor("kubernetes", "deployment", "degraded")
	if err != nil {
		t.Fatalf("FailureModesFor: %v", err)
	}
	if len(causes) == 0 {
		t.Error("FailureModesFor degraded: expected non-empty causes")
	}
	foundUpstream := false
	for _, c := range causes {
		if c == "upstream_failure" {
			foundUpstream = true
		}
	}
	if !foundUpstream {
		t.Errorf("FailureModesFor degraded: upstream_failure not in %v", causes)
	}

	// HealthyConditionsFor
	healthy, err := reg.HealthyConditionsFor("kubernetes", "deployment")
	if err != nil {
		t.Fatalf("HealthyConditionsFor: %v", err)
	}
	if len(healthy) != 3 {
		t.Errorf("HealthyConditionsFor = %v, want 3 conditions", healthy)
	}

	// FactsFor
	facts, err := reg.FactsFor("kubernetes", "deployment")
	if err != nil {
		t.Fatalf("FactsFor: %v", err)
	}
	if len(facts) != 4 {
		t.Errorf("FactsFor: count = %d, want 4", len(facts))
	}

	// StatesFor
	states, err := reg.StatesFor("kubernetes", "deployment")
	if err != nil {
		t.Fatalf("StatesFor: %v", err)
	}
	if len(states) != 4 {
		t.Errorf("StatesFor: count = %d, want 4", len(states))
	}
	if states[0].Name != "degraded" {
		t.Errorf("StatesFor[0].Name = %q, want degraded", states[0].Name)
	}

	// ProbeCostFor
	cost, err := reg.ProbeCostFor("kubernetes", "deployment", "ready_replicas")
	if err != nil {
		t.Fatalf("ProbeCostFor: %v", err)
	}
	if cost != "low" {
		t.Errorf("ProbeCostFor = %q, want low", cost)
	}

	// ProbeCostFor — unknown fact
	_, err = reg.ProbeCostFor("kubernetes", "deployment", "nonexistent_fact")
	if err == nil {
		t.Error("expected error for nonexistent fact, got nil")
	}
}

func TestLoadProvider_CompiledExpressions(t *testing.T) {
	k8s, err := LoadFromFile("testdata/kubernetes.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile kubernetes: %v", err)
	}

	deploy, ok := k8s.Types["deployment"]
	if !ok {
		t.Fatal("missing type deployment")
	}

	// Verify deployment.Healthy has 3 compiled nodes (non-nil).
	if len(deploy.Healthy) != 3 {
		t.Errorf("deployment.Healthy compiled nodes = %d, want 3", len(deploy.Healthy))
	}
	for i, node := range deploy.Healthy {
		if node == nil {
			t.Errorf("deployment.Healthy[%d] is nil", i)
		}
	}

	// Verify deployment.States[0] is "degraded" and has non-nil When.
	if len(deploy.States) == 0 {
		t.Fatal("deployment has no states")
	}
	if deploy.States[0].Name != "degraded" {
		t.Errorf("deployment.States[0].Name = %q, want degraded", deploy.States[0].Name)
	}
	if deploy.States[0].When == nil {
		t.Error("deployment.States[0].When (degraded) is nil, want compiled node")
	}

	// Verify all states with WhenRaw have compiled When nodes.
	for _, sd := range deploy.States {
		if sd.WhenRaw != "" && sd.When == nil {
			t.Errorf("state %q has WhenRaw %q but When is nil", sd.Name, sd.WhenRaw)
		}
	}

	// Verify HealthyRaw is still present (for display).
	if len(deploy.HealthyRaw) != 3 {
		t.Errorf("deployment.HealthyRaw = %d, want 3 (raw strings must be kept)", len(deploy.HealthyRaw))
	}
}

func TestRegistry_GetAndAll(t *testing.T) {
	reg, k8s, aws := loadTestProviders(t)

	p, ok := reg.Get("kubernetes")
	if !ok {
		t.Fatal("Get kubernetes: not found")
	}
	if p != k8s {
		t.Error("Get kubernetes: returned wrong provider")
	}

	all := reg.All()
	if len(all) != 2 {
		t.Errorf("All() count = %d, want 2", len(all))
	}
	_ = aws
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

func TestLoadFromDir_KubernetesProvider(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir kubernetes: %v", err)
	}

	if p.Meta.Name != "kubernetes" {
		t.Errorf("Meta.Name = %q, want kubernetes", p.Meta.Name)
	}
	if p.Meta.Version != "2.0.0" {
		t.Errorf("Meta.Version = %q, want 2.0.0", p.Meta.Version)
	}
	if p.Variables["namespace"].Default != "default" {
		t.Errorf("namespace default = %q, want default", p.Variables["namespace"].Default)
	}
}

func TestKubernetesProvider_WorkloadTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	workloads := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"deployment":  {wantFacts: 8, wantStates: 6, defaultSt: "live"},
		"statefulset": {wantFacts: 7, wantStates: 6, defaultSt: "live"},
		"daemonset":   {wantFacts: 6, wantStates: 4, defaultSt: "live"},
		"replicaset":  {wantFacts: 3, wantStates: 3, defaultSt: "live"},
	}

	for typeName, want := range workloads {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}

func TestKubernetesProvider_ScheduledWorkloadTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"cronjob": {wantFacts: 4, wantStates: 5, defaultSt: "live"},
		"job":     {wantFacts: 6, wantStates: 4, defaultSt: "complete"},
		"pod":     {wantFacts: 6, wantStates: 6, defaultSt: "running"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}

func TestKubernetesProvider_NetworkingTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"service":       {wantFacts: 4, wantStates: 3, defaultSt: "live"},
		"ingress":       {wantFacts: 4, wantStates: 3, defaultSt: "live"},
		"ingressclass":  {wantFacts: 2, wantStates: 2, defaultSt: "ready"},
		"endpoints":     {wantFacts: 2, wantStates: 3, defaultSt: "live"},
		"networkpolicy": {wantFacts: 4, wantStates: 3, defaultSt: "active"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}

func TestKubernetesProvider_ScalingAndStorageTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"hpa":              {wantFacts: 8, wantStates: 4, defaultSt: "live"},
		"pdb":              {wantFacts: 4, wantStates: 3, defaultSt: "live"},
		"pvc":              {wantFacts: 4, wantStates: 3, defaultSt: "bound"},
		"persistentvolume": {wantFacts: 3, wantStates: 5, defaultSt: "bound"},
		"storageclass":     {wantFacts: 3, wantStates: 2, defaultSt: "ready"},
		"csidriver":        {wantFacts: 2, wantStates: 3, defaultSt: "ready"},
		"volumeattachment": {wantFacts: 3, wantStates: 4, defaultSt: "attached"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}

func TestKubernetesProvider_ClusterTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"node":          {wantFacts: 9, wantStates: 5, defaultSt: "ready"},
		"resourcequota": {wantFacts: 7, wantStates: 3, defaultSt: "active"},
		"limitrange":    {wantFacts: 3, wantStates: 2, defaultSt: "active"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}

func TestKubernetesProvider_PrerequisiteTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"namespace":      {wantFacts: 2, wantStates: 3, defaultSt: "active"},
		"serviceaccount": {wantFacts: 3, wantStates: 3, defaultSt: "ready"},
		"secret":         {wantFacts: 3, wantStates: 3, defaultSt: "ready"},
		"configmap":      {wantFacts: 3, wantStates: 3, defaultSt: "ready"},
		"operator":       {wantFacts: 4, wantStates: 4, defaultSt: "ready"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}

func TestKubernetesProvider_RBACTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"role":               {wantFacts: 2, wantStates: 2, defaultSt: "ready"},
		"clusterrole":        {wantFacts: 2, wantStates: 2, defaultSt: "ready"},
		"rolebinding":        {wantFacts: 3, wantStates: 3, defaultSt: "ready"},
		"clusterrolebinding": {wantFacts: 3, wantStates: 3, defaultSt: "ready"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}

func TestKubernetesProvider_WebhookTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"validatingwebhookconfiguration": {wantFacts: 4, wantStates: 4, defaultSt: "active"},
		"mutatingwebhookconfiguration":   {wantFacts: 4, wantStates: 4, defaultSt: "active"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}

func TestKubernetesProvider_ExtensibilityTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"customresourcedefinition": {wantFacts: 3, wantStates: 4, defaultSt: "ready"},
		"priorityclass":           {wantFacts: 3, wantStates: 2, defaultSt: "ready"},
		"lease":                   {wantFacts: 4, wantStates: 3, defaultSt: "held"},
		"custom_resource":         {wantFacts: 4, wantStates: 4, defaultSt: "ready"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}

func TestLoadFromDir_FallsBackToInlineTypes(t *testing.T) {
	dir := t.TempDir()
	data, err := os.ReadFile("testdata/kubernetes.yaml")
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

	if p.Meta.Name != "kubernetes" {
		t.Errorf("Meta.Name = %q, want kubernetes", p.Meta.Name)
	}
	if _, ok := p.Types["deployment"]; !ok {
		t.Fatal("missing type deployment — inline types not loaded")
	}
}
