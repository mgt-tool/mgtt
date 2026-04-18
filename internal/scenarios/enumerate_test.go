package scenarios

import (
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// buildRegistry wires a single synthetic provider into a registry with
// the given types pre-populated. Keeps tests compact by avoiding YAML.
func buildRegistry(providerName string, types map[string]*providersupport.Type) *providersupport.Registry {
	p := &providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: providerName, Version: "0.1.0", Description: "test"},
		Types: types,
	}
	reg := providersupport.NewRegistry()
	reg.Register(p)
	return reg
}

// testType is a small helper to build a *providersupport.Type with the
// fields the enumerator reads: States, FailureModes, Facts,
// DefaultActiveState.
func testType(name, defaultState string, facts []string, states []providersupport.StateDef, failureModes map[string][]string) *providersupport.Type {
	factMap := map[string]*providersupport.FactSpec{}
	for _, f := range facts {
		factMap[f] = &providersupport.FactSpec{TypeName: "mgtt.int", TTL: time.Minute}
	}
	return &providersupport.Type{
		Name:               name,
		Facts:              factMap,
		States:             states,
		DefaultActiveState: defaultState,
		FailureModes:       failureModes,
	}
}

// buildModel builds a model.Model where edges are expressed as
// {downstream: [upstream1, upstream2]} — i.e. "downstream depends on these
// upstreams." Component types are given by compTypes.
func buildModel(providers []string, compTypes map[string]string, depends map[string][]string) *model.Model {
	components := map[string]*model.Component{}
	var order []string
	for name, typ := range compTypes {
		components[name] = &model.Component{Name: name, Type: typ}
		order = append(order, name)
	}
	for down, ups := range depends {
		c := components[down]
		if c == nil {
			continue
		}
		c.Depends = append(c.Depends, model.Dependency{On: ups})
	}
	m := &model.Model{
		Meta:       model.Meta{Providers: providers},
		Components: components,
		Order:      order,
	}
	return m
}

// TestEnumerate_TwoStepChain — upstream.stopped emits "timeout";
// downstream.crashed has triggered_by [timeout]; enumerator must build a
// two-step chain carrying EmitsOnEdge=timeout then an Observes terminal.
func TestEnumerate_TwoStepChain(t *testing.T) {
	upstreamType := testType("db", "live",
		[]string{"conn_count"},
		[]providersupport.StateDef{
			{Name: "stopped"},
			{Name: "live"},
		},
		map[string][]string{"stopped": {"timeout"}},
	)
	downstreamType := testType("api", "live",
		[]string{"error_rate"},
		[]providersupport.StateDef{
			{Name: "crashed", TriggeredBy: []string{"timeout"}},
			{Name: "live"},
		},
		map[string][]string{"crashed": {}},
	)
	reg := buildRegistry("p", map[string]*providersupport.Type{
		"db":  upstreamType,
		"api": downstreamType,
	})
	m := buildModel(
		[]string{"p"},
		map[string]string{"db": "db", "api": "api"},
		map[string][]string{"api": {"db"}},
	)

	scenarios := Enumerate(m, reg)
	if len(scenarios) == 0 {
		t.Fatal("expected at least one scenario")
	}

	var twoStep *Scenario
	for i := range scenarios {
		s := scenarios[i]
		if s.Root.Component == "db" && s.Root.State == "stopped" && s.Length() == 2 {
			twoStep = &scenarios[i]
			break
		}
	}
	if twoStep == nil {
		t.Fatalf("no two-step db.stopped chain found; got %+v", scenarios)
	}
	if twoStep.Chain[0].Component != "db" || twoStep.Chain[0].State != "stopped" || twoStep.Chain[0].EmitsOnEdge != "timeout" {
		t.Errorf("step 0 wrong: %+v", twoStep.Chain[0])
	}
	if twoStep.Chain[1].Component != "api" || twoStep.Chain[1].State != "crashed" {
		t.Errorf("step 1 wrong: %+v", twoStep.Chain[1])
	}
	if len(twoStep.Chain[1].Observes) == 0 {
		t.Errorf("terminal step should carry observes; got %+v", twoStep.Chain[1])
	}
}

// TestEnumerate_DeterministicIDs — two runs over the same model produce
// identical ID → chainKey bindings.
func TestEnumerate_DeterministicIDs(t *testing.T) {
	upstreamType := testType("db", "live",
		[]string{"conn_count"},
		[]providersupport.StateDef{
			{Name: "stopped"},
			{Name: "live"},
		},
		map[string][]string{"stopped": {"timeout"}},
	)
	downstreamType := testType("api", "live",
		[]string{"error_rate"},
		[]providersupport.StateDef{
			{Name: "crashed", TriggeredBy: []string{"timeout"}},
			{Name: "live"},
		},
		map[string][]string{"crashed": {}},
	)
	reg := buildRegistry("p", map[string]*providersupport.Type{
		"db":  upstreamType,
		"api": downstreamType,
	})
	m := buildModel(
		[]string{"p"},
		map[string]string{"db": "db", "api": "api"},
		map[string][]string{"api": {"db"}},
	)

	run1 := Enumerate(m, reg)
	run2 := Enumerate(m, reg)
	if len(run1) != len(run2) {
		t.Fatalf("run1=%d run2=%d scenarios", len(run1), len(run2))
	}
	for i := range run1 {
		if run1[i].ID != run2[i].ID {
			t.Errorf("id[%d] differs: %s vs %s", i, run1[i].ID, run2[i].ID)
		}
		if chainKey(run1[i].Chain) != chainKey(run2[i].Chain) {
			t.Errorf("chain[%d] differs: %s vs %s", i, chainKey(run1[i].Chain), chainKey(run2[i].Chain))
		}
	}
}

// TestEnumerate_PermissiveDefault — downstream crashed has NO triggered_by;
// the chain must still be produced because permissive default matches any
// upstream label.
func TestEnumerate_PermissiveDefault(t *testing.T) {
	upstreamType := testType("db", "live",
		[]string{"conn_count"},
		[]providersupport.StateDef{
			{Name: "stopped"},
			{Name: "live"},
		},
		map[string][]string{"stopped": {"timeout"}},
	)
	downstreamType := testType("api", "live",
		[]string{"error_rate"},
		[]providersupport.StateDef{
			{Name: "crashed"}, // NO triggered_by
			{Name: "live"},
		},
		map[string][]string{"crashed": {}},
	)
	reg := buildRegistry("p", map[string]*providersupport.Type{
		"db":  upstreamType,
		"api": downstreamType,
	})
	m := buildModel(
		[]string{"p"},
		map[string]string{"db": "db", "api": "api"},
		map[string][]string{"api": {"db"}},
	)

	scenarios := Enumerate(m, reg)
	found := false
	for _, s := range scenarios {
		if s.Root.Component == "db" && s.Root.State == "stopped" &&
			s.Length() == 2 &&
			s.Chain[1].Component == "api" && s.Chain[1].State == "crashed" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("permissive default should still build db.stopped → api.crashed; got %+v", scenarios)
	}
}

// TestEnumerate_DiamondProducesBothBranches — entry → left → leaf AND
// entry → right → leaf. Enumerator must emit scenarios for both branches.
func TestEnumerate_DiamondProducesBothBranches(t *testing.T) {
	entryType := testType("entry", "live",
		[]string{"fact_entry"},
		[]providersupport.StateDef{
			{Name: "down"},
			{Name: "live"},
		},
		map[string][]string{"down": {"outage"}},
	)
	leftType := testType("left", "live",
		[]string{"fact_left"},
		[]providersupport.StateDef{
			{Name: "broken", TriggeredBy: []string{"outage"}},
			{Name: "live"},
		},
		map[string][]string{"broken": {"leftfail"}},
	)
	rightType := testType("right", "live",
		[]string{"fact_right"},
		[]providersupport.StateDef{
			{Name: "broken", TriggeredBy: []string{"outage"}},
			{Name: "live"},
		},
		map[string][]string{"broken": {"rightfail"}},
	)
	leafType := testType("leaf", "live",
		[]string{"fact_leaf"},
		[]providersupport.StateDef{
			{Name: "sad", TriggeredBy: []string{"leftfail", "rightfail"}},
			{Name: "live"},
		},
		map[string][]string{"sad": {}},
	)
	reg := buildRegistry("p", map[string]*providersupport.Type{
		"entry": entryType,
		"left":  leftType,
		"right": rightType,
		"leaf":  leafType,
	})
	// leaf depends on left and right; left and right both depend on entry.
	m := buildModel(
		[]string{"p"},
		map[string]string{
			"entry": "entry",
			"left":  "left",
			"right": "right",
			"leaf":  "leaf",
		},
		map[string][]string{
			"left":  {"entry"},
			"right": {"entry"},
			"leaf":  {"left", "right"},
		},
	)

	scenarios := Enumerate(m, reg)
	viaLeft := false
	viaRight := false
	for _, s := range scenarios {
		if !s.TouchesComponent("leaf") {
			continue
		}
		if s.TouchesComponent("left") {
			viaLeft = true
		}
		if s.TouchesComponent("right") {
			viaRight = true
		}
	}
	if !viaLeft {
		t.Error("no scenario reached leaf via left branch")
	}
	if !viaRight {
		t.Error("no scenario reached leaf via right branch")
	}
}

// TestEnumerate_CycleTerminates — A depends on B, B depends on A. Enumerate
// must not diverge; completes within the timeout.
func TestEnumerate_CycleTerminates(t *testing.T) {
	aType := testType("a", "live",
		[]string{"fact_a"},
		[]providersupport.StateDef{
			{Name: "bad"},
			{Name: "live"},
		},
		map[string][]string{"bad": {"label_a"}},
	)
	bType := testType("b", "live",
		[]string{"fact_b"},
		[]providersupport.StateDef{
			{Name: "bad"},
			{Name: "live"},
		},
		map[string][]string{"bad": {"label_b"}},
	)
	reg := buildRegistry("p", map[string]*providersupport.Type{
		"a": aType,
		"b": bType,
	})
	// Mutual dependency.
	m := buildModel(
		[]string{"p"},
		map[string]string{"a": "a", "b": "b"},
		map[string][]string{
			"a": {"b"},
			"b": {"a"},
		},
	)

	done := make(chan struct{})
	var scenarios []Scenario
	go func() {
		scenarios = Enumerate(m, reg)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("enumerate did not complete within 2 seconds — cycle likely diverged")
	}

	// Just a sanity check: the output should be non-nil (not required
	// to contain any specific scenario, just that it terminated).
	_ = scenarios
}
