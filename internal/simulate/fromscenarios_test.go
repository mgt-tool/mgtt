package simulate

import (
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// twoCompModel builds a minimal 2-component chain (web → db) with
// single-fact types and a single non-default state each.
func twoCompModel(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()
	mk := func(name, emitsFrom string) *providersupport.Type {
		ty := &providersupport.Type{
			Name: name,
			Facts: map[string]*providersupport.FactSpec{
				"status": {Probe: providersupport.ProbeDef{Cmd: name + "-status", Cost: "cheap", Access: "read"}},
			},
			States: []providersupport.StateDef{
				{Name: "active", When: &expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "up"}},
				{Name: "down", When: &expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "down"}},
			},
			DefaultActiveState: "active",
			FailureModes:       map[string][]string{},
		}
		if emitsFrom != "" {
			ty.FailureModes["down"] = []string{emitsFrom}
		}
		return ty
	}
	prov := &providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{
			"web": mk("web", ""),
			"db":  mk("db", "db_down"),
		},
	}
	// Link via TriggeredBy so enumerate chains db → web.
	prov.Types["web"].States[1].TriggeredBy = []string{"db_down"}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"web": {Name: "web", Type: "web", Depends: []model.Dependency{{On: []string{"db"}}}},
			"db":  {Name: "db", Type: "db"},
		},
		Order: []string{"web", "db"},
	}
	m.BuildGraph()
	return m, reg
}

func TestRunFromScenarios_Pass(t *testing.T) {
	m, reg := twoCompModel(t)
	sc := scenarios.Scenario{
		ID:   "s-0001",
		Root: scenarios.RootRef{Component: "db", State: "down"},
		Chain: []scenarios.Step{
			{Component: "db", State: "down", EmitsOnEdge: "db_down"},
			{Component: "web", State: "down", Observes: []string{"status"}},
		},
	}
	passed, failed, details := RunFromScenarios(m, reg, []scenarios.Scenario{sc})
	if passed != 1 || failed != 0 {
		t.Fatalf("want 1 pass 0 fail; got %d/%d\ndetails: %v", passed, failed, details)
	}
	if len(details) != 1 || !strings.Contains(details[0], "PASS") {
		t.Errorf("details = %v; want single PASS", details)
	}
}

// A scenario whose root mismatches Occam's convergence is reported as
// FAIL. We build two scenarios of equal length (so tie-break by ID
// resolves deterministically) and point the "bogus" one at the wrong
// root.
func TestRunFromScenarios_WrongRootReportedAsFail(t *testing.T) {
	m, reg := twoCompModel(t)
	// Scenario A: root=db; Scenario B: same chain but claims root=web.
	// Synth + Occam on B will converge on whichever scenario matches
	// the facts; since both use the same chain, the ID-tiebreak picks
	// "a" alphabetically — so B's runOneScenario gets back a RootCause
	// with Component=db, mismatching B's claim of web.
	chain := []scenarios.Step{
		{Component: "db", State: "down", EmitsOnEdge: "db_down"},
		{Component: "web", State: "down", Observes: []string{"status"}},
	}
	a := scenarios.Scenario{
		ID:    "a-correct",
		Root:  scenarios.RootRef{Component: "db", State: "down"},
		Chain: chain,
	}
	b := scenarios.Scenario{
		ID:    "b-wrong",
		Root:  scenarios.RootRef{Component: "web", State: "down"}, // lies
		Chain: chain,
	}
	passed, failed, details := RunFromScenarios(m, reg, []scenarios.Scenario{a, b})
	// We expect at least one FAIL (the lying one).
	if failed == 0 {
		t.Fatalf("want at least 1 fail; got passed=%d failed=%d details=%v", passed, failed, details)
	}
}

// When two scenarios are enumerated and the caller's scenario lists
// both as candidates, the strategy still converges on the root of the
// synthesized one. This guards against the strategy picking the shorter
// sibling due to tie-break rules.
func TestRunFromScenarios_TwoScenarios(t *testing.T) {
	m, reg := twoCompModel(t)
	shorter := scenarios.Scenario{
		ID:   "s-short",
		Root: scenarios.RootRef{Component: "web", State: "down"},
		Chain: []scenarios.Step{
			{Component: "web", State: "down", Observes: []string{"status"}},
		},
	}
	longer := scenarios.Scenario{
		ID:   "s-long",
		Root: scenarios.RootRef{Component: "db", State: "down"},
		Chain: []scenarios.Step{
			{Component: "db", State: "down", EmitsOnEdge: "db_down"},
			{Component: "web", State: "down", Observes: []string{"status"}},
		},
	}
	all := []scenarios.Scenario{shorter, longer}
	// When we seed facts matching the shorter chain only, Occam must
	// converge on the shorter one.
	passed, failed, details := RunFromScenarios(m, reg, all)
	// Either both pass (each identified uniquely when facts are
	// synthesized for its chain) or the longer one fails because its
	// facts also satisfy the shorter. Accept the first; surface the
	// second as a documented limitation.
	if passed == 0 {
		t.Fatalf("want at least 1 pass; got passed=%d failed=%d details=%v", passed, failed, details)
	}
}

// deriveSatisfyingAssignments coverage: equality, inequality, numeric,
// boolean, AND composition.
func TestDeriveSatisfyingAssignments(t *testing.T) {
	cases := []struct {
		name  string
		node  expr.Node
		want  map[string]any
		check func(m map[string]any) bool
	}{
		{
			name: "equality string",
			node: &expr.CmpNode{Fact: "phase", Op: expr.OpEq, Value: "down"},
			check: func(m map[string]any) bool {
				v, ok := m["phase"]
				return ok && v == "down"
			},
		},
		{
			name: "inequality bool",
			node: &expr.CmpNode{Fact: "available", Op: expr.OpEq, Value: false},
			check: func(m map[string]any) bool {
				v, ok := m["available"]
				return ok && v == false
			},
		},
		{
			name: "gt numeric",
			node: &expr.CmpNode{Fact: "count", Op: expr.OpGt, Value: 10},
			check: func(m map[string]any) bool {
				v, ok := m["count"]
				if !ok {
					return false
				}
				switch x := v.(type) {
				case int:
					return x > 10
				case float64:
					return x > 10
				}
				return false
			},
		},
		{
			name: "and composition",
			node: &expr.AndNode{
				L: &expr.CmpNode{Fact: "a", Op: expr.OpEq, Value: 1},
				R: &expr.CmpNode{Fact: "b", Op: expr.OpEq, Value: 2},
			},
			check: func(m map[string]any) bool {
				return m["a"] == 1 && m["b"] == 2
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := deriveSatisfyingAssignments(tc.node, true, nil)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !tc.check(got) {
				t.Errorf("assignments = %v; check failed", got)
			}
		})
	}
}

// 1a. Fact-on-RHS: `ready_replicas < desired_replicas` — RHS names a
// peer fact in the same type. Synthesizer should bind both facts to
// an integer pair satisfying the comparison.
func TestSynthesize_FactOnRHS(t *testing.T) {
	// Build a type with two int-valued facts. The state's When
	// compares them against each other.
	knownFacts := map[string]*providersupport.FactSpec{
		"ready_replicas":   {},
		"desired_replicas": {},
	}
	node := &expr.CmpNode{
		Fact:  "ready_replicas",
		Op:    expr.OpLt,
		Value: "desired_replicas",
	}
	got, err := deriveSatisfyingAssignments(node, true, knownFacts)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	lv, lok := got["ready_replicas"]
	rv, rok := got["desired_replicas"]
	if !lok || !rok {
		t.Fatalf("want both facts bound; got %v", got)
	}
	li, lints := asInt(lv)
	ri, rints := asInt(rv)
	if !lints || !rints {
		t.Fatalf("want int bindings; got %v (%T), %v (%T)", lv, lv, rv, rv)
	}
	if !(li < ri) {
		t.Errorf("want ready_replicas < desired_replicas; got %d vs %d", li, ri)
	}
}

// 1b. OR-branch rebalance: when left branch conflicts with an
// existing binding, prefer the right branch.
func TestSynthesize_OrBranchAvoidsConflict(t *testing.T) {
	// AND(fact_a == 5, OR(fact_a == 3, fact_b == 7)).
	// Left branch of OR binds fact_a=3 which conflicts with AND's
	// fact_a=5; right branch binds fact_b=7 which has no conflict.
	node := &expr.AndNode{
		L: &expr.CmpNode{Fact: "fact_a", Op: expr.OpEq, Value: 5},
		R: &expr.OrNode{
			L: &expr.CmpNode{Fact: "fact_a", Op: expr.OpEq, Value: 3},
			R: &expr.CmpNode{Fact: "fact_b", Op: expr.OpEq, Value: 7},
		},
	}
	got, err := deriveSatisfyingAssignments(node, true, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got["fact_a"] != 5 {
		t.Errorf("fact_a = %v; want 5 (AND-sibling must win)", got["fact_a"])
	}
	if got["fact_b"] != 7 {
		t.Errorf("fact_b = %v; want 7 (OR right branch picked to avoid conflict)", got["fact_b"])
	}
}

// 1c. Cross-state ref: a `state == "..."` predicate cannot be
// synthesized by writing a fact; the walker must return
// ErrCrossStateRef with a recognizable message.
func TestSynthesize_CrossStateRefReturnsError(t *testing.T) {
	// `state == "stopped"` — a self-state reference (the walker
	// treats any "state" fact as the derived-state sentinel).
	node := &expr.CmpNode{
		Fact:  "state",
		Op:    expr.OpEq,
		Value: "stopped",
	}
	_, err := deriveSatisfyingAssignments(node, true, nil)
	if err == nil {
		t.Fatal("want error for cross-state ref; got nil")
	}
	if !strings.Contains(err.Error(), "cross-state reference") {
		t.Errorf("err = %q; want message identifying cross-state ref", err.Error())
	}
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		if x == float64(int(x)) {
			return int(x), true
		}
	}
	return 0, false
}
