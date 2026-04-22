package expr_test

import (
	"errors"
	"testing"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
)

// makeCtx builds an expr.Ctx from a simple map of component→key→value plus a
// state map.
func makeCtx(component string, factsMap map[string]map[string]any, states map[string]string) expr.Ctx {
	store := facts.NewInMemory()
	for c, kvs := range factsMap {
		for k, v := range kvs {
			store.Append(c, facts.Fact{Key: k, Value: v})
		}
	}
	return expr.Ctx{CurrentComponent: component, Facts: store, States: states}
}

// ---------------------------------------------------------------------------
// Parser tests
// ---------------------------------------------------------------------------

func TestParseSimpleComparison(t *testing.T) {
	node, err := expr.Parse("ready_replicas == 3")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if node == nil {
		t.Fatal("expected non-nil node")
	}
	cmp, ok := node.(*expr.CmpNode)
	if !ok {
		t.Fatalf("expected *CmpNode, got %T", node)
	}
	if cmp.Fact != "ready_replicas" {
		t.Errorf("fact: got %q want %q", cmp.Fact, "ready_replicas")
	}
	if cmp.Op != expr.OpEq {
		t.Errorf("op: got %v want OpEq", cmp.Op)
	}
	if cmp.Value != 3 {
		t.Errorf("value: got %v, want 3", cmp.Value)
	}
}

func TestParseAnd(t *testing.T) {
	node, err := expr.Parse("ready_replicas < desired_replicas & restart_count > 5")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	_, ok := node.(*expr.AndNode)
	if !ok {
		t.Fatalf("expected *AndNode, got %T", node)
	}
}

func TestParseOr(t *testing.T) {
	node, err := expr.Parse("a == 1 | b == 2")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	_, ok := node.(*expr.OrNode)
	if !ok {
		t.Fatalf("expected *OrNode, got %T", node)
	}
}

func TestParseComponentFact(t *testing.T) {
	node, err := expr.Parse("api.ready_replicas == 0")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	cmp, ok := node.(*expr.CmpNode)
	if !ok {
		t.Fatalf("expected *CmpNode, got %T", node)
	}
	if cmp.Component != "api" {
		t.Errorf("component: got %q want %q", cmp.Component, "api")
	}
	if cmp.Fact != "ready_replicas" {
		t.Errorf("fact: got %q want %q", cmp.Fact, "ready_replicas")
	}
}

func TestParseStateComparison(t *testing.T) {
	node, err := expr.Parse("vault.state == starting")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	cmp, ok := node.(*expr.CmpNode)
	if !ok {
		t.Fatalf("expected *CmpNode, got %T", node)
	}
	if cmp.Component != "vault" {
		t.Errorf("component: got %q want %q", cmp.Component, "vault")
	}
	if cmp.Fact != "state" {
		t.Errorf("fact: got %q want %q", cmp.Fact, "state")
	}
	if cmp.Value != "starting" {
		t.Errorf("value: got %v, want starting", cmp.Value)
	}
}

func TestParseBool(t *testing.T) {
	node, err := expr.Parse("available == true")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	cmp, ok := node.(*expr.CmpNode)
	if !ok {
		t.Fatalf("expected *CmpNode, got %T", node)
	}
	if cmp.Value != true {
		t.Errorf("value: got %v, want true", cmp.Value)
	}
}

func TestParseParentheses(t *testing.T) {
	// (a == 1 | b == 2) & c == 3  →  AND at the top
	node, err := expr.Parse("(a == 1 | b == 2) & c == 3")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	and, ok := node.(*expr.AndNode)
	if !ok {
		t.Fatalf("expected *AndNode at top level, got %T", node)
	}
	_, ok = and.L.(*expr.OrNode)
	if !ok {
		t.Fatalf("expected *OrNode on left side of AND, got %T", and.L)
	}
}

// ---------------------------------------------------------------------------
// Evaluator tests
// ---------------------------------------------------------------------------

func TestEvalSimpleTrue(t *testing.T) {
	node, _ := expr.Parse("ready_replicas == 3")
	ctx := makeCtx("api", map[string]map[string]any{
		"api": {"ready_replicas": 3},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true")
	}
}

func TestEvalSimpleFalse(t *testing.T) {
	node, _ := expr.Parse("ready_replicas == 3")
	ctx := makeCtx("api", map[string]map[string]any{
		"api": {"ready_replicas": 5},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error("expected false")
	}
}

func TestEvalUnresolvedMissingFact(t *testing.T) {
	node, _ := expr.Parse("ready_replicas == 3")
	ctx := makeCtx("api", map[string]map[string]any{}, nil)
	result, err := node.Eval(ctx)
	if result {
		t.Error("expected false for unresolved")
	}
	var ue *expr.UnresolvedError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *UnresolvedError, got %T: %v", err, err)
	}
	if ue.Reason != "missing" {
		t.Errorf("reason: got %q want %q", ue.Reason, "missing")
	}
}

func TestEvalAndBothTrue(t *testing.T) {
	node, _ := expr.Parse("a == 1 & b == 2")
	ctx := makeCtx("svc", map[string]map[string]any{
		"svc": {"a": 1, "b": 2},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true")
	}
}

func TestEvalAndOneUnresolved(t *testing.T) {
	// a is present (true), b is missing → unresolved propagates
	node, _ := expr.Parse("a == 1 & b == 2")
	ctx := makeCtx("svc", map[string]map[string]any{
		"svc": {"a": 1},
	}, nil)
	result, err := node.Eval(ctx)
	if result {
		t.Error("expected false for unresolved AND")
	}
	var ue *expr.UnresolvedError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *UnresolvedError, got %T: %v", err, err)
	}
}

func TestEvalAndOneFalseOneUnresolved(t *testing.T) {
	// a is false (value mismatch), b is missing
	// Result: (false, nil) — definitely false, not unresolved
	node, _ := expr.Parse("a == 99 & b == 2")
	ctx := makeCtx("svc", map[string]map[string]any{
		"svc": {"a": 1},
	}, nil)
	result, err := node.Eval(ctx)
	if result {
		t.Error("expected false")
	}
	if err != nil {
		t.Errorf("expected nil error (definitely false), got %v", err)
	}
}

func TestEvalStateComparison(t *testing.T) {
	node, _ := expr.Parse("vault.state == starting")
	ctx := makeCtx("api", nil, map[string]string{
		"vault": "starting",
	})
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true")
	}
}

func TestEvalStateComparisonFalse(t *testing.T) {
	node, _ := expr.Parse("vault.state == starting")
	ctx := makeCtx("api", nil, map[string]string{
		"vault": "running",
	})
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error("expected false")
	}
}

func TestEvalStateMissing(t *testing.T) {
	node, _ := expr.Parse("vault.state == starting")
	ctx := makeCtx("api", nil, map[string]string{})
	result, err := node.Eval(ctx)
	if result {
		t.Error("expected false for missing state")
	}
	var ue *expr.UnresolvedError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *UnresolvedError for missing state, got %T: %v", err, err)
	}
	if ue.Fact != "state" {
		t.Errorf("fact: got %q want %q", ue.Fact, "state")
	}
}

func TestEvalBoolFact(t *testing.T) {
	node, _ := expr.Parse("available == true")
	ctx := makeCtx("api", map[string]map[string]any{
		"api": {"available": true},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true")
	}
}

func TestEvalCrossComponentRef(t *testing.T) {
	// Evaluating "api.ready_replicas == 0" from the edge component context.
	node, _ := expr.Parse("api.ready_replicas == 0")
	ctx := makeCtx("edge", map[string]map[string]any{
		"api": {"ready_replicas": 0},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true")
	}
}

func TestEvalK8sDegraded(t *testing.T) {
	// ready_replicas < desired_replicas & restart_count > 5
	node, _ := expr.Parse("ready_replicas < desired_replicas & restart_count > 5")
	ctx := makeCtx("api", map[string]map[string]any{
		"api": {
			"ready_replicas":   2,
			"desired_replicas": 3,
			"restart_count":    10,
		},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true (degraded condition)")
	}
}

func TestEvalK8sStarting(t *testing.T) {
	// ready_replicas < desired_replicas — with just these two facts, no restart_count
	node, _ := expr.Parse("ready_replicas < desired_replicas")
	ctx := makeCtx("api", map[string]map[string]any{
		"api": {
			"ready_replicas":   0,
			"desired_replicas": 3,
		},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true (starting condition)")
	}
}

// ---------------------------------------------------------------------------
// Additional edge-case evaluator tests
// ---------------------------------------------------------------------------

func TestEvalOrShortCircuitTrue(t *testing.T) {
	// Left is true → whole OR is true, right is missing but irrelevant
	node, _ := expr.Parse("a == 1 | b == 99")
	ctx := makeCtx("svc", map[string]map[string]any{
		"svc": {"a": 1},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true")
	}
}

func TestEvalOrLeftFalseRightTrue(t *testing.T) {
	node, _ := expr.Parse("a == 99 | b == 2")
	ctx := makeCtx("svc", map[string]map[string]any{
		"svc": {"a": 1, "b": 2},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true")
	}
}

func TestEvalOrUnresolvedLeftTrueRight(t *testing.T) {
	// Left is unresolved, right is true → OR is true
	node, _ := expr.Parse("missing_fact == 1 | b == 2")
	ctx := makeCtx("svc", map[string]map[string]any{
		"svc": {"b": 2},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true (unresolved | true)")
	}
}

func TestEvalFloatComparison(t *testing.T) {
	node, _ := expr.Parse("cpu == 1.5")
	ctx := makeCtx("api", map[string]map[string]any{
		"api": {"cpu": 1.5},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true for float comparison")
	}
}

func TestEvalIntFloatPromotion(t *testing.T) {
	// fact is int, value is float
	node, _ := expr.Parse("count == 3.0")
	ctx := makeCtx("api", map[string]map[string]any{
		"api": {"count": 3},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true for int/float promotion")
	}
}

func TestEvalStringFactNeq(t *testing.T) {
	node, _ := expr.Parse("status != healthy")
	ctx := makeCtx("api", map[string]map[string]any{
		"api": {"status": "degraded"},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true (degraded != healthy)")
	}
}

func TestParseDoubleQuotedStringLiteral(t *testing.T) {
	// Quoted string literal: token "unhealthy" becomes string value "unhealthy"
	// (no bool/int inference, no stripping of special characters).
	node, err := expr.Parse(`health_status != "unhealthy"`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	cmp, ok := node.(*expr.CmpNode)
	if !ok {
		t.Fatalf("expected *CmpNode, got %T", node)
	}
	if cmp.Fact != "health_status" {
		t.Errorf("fact: got %q want %q", cmp.Fact, "health_status")
	}
	if cmp.Op != expr.OpNeq {
		t.Errorf("op: got %v want OpNeq", cmp.Op)
	}
	if cmp.Value != "unhealthy" {
		t.Errorf("value: got %v (%T), want string %q", cmp.Value, cmp.Value, "unhealthy")
	}
}

func TestParseQuotedDefeatsInference(t *testing.T) {
	// A quoted "42" must remain the string "42", not the int 42 — that's the
	// whole point of quoting. Same for "true".
	cases := []struct {
		expr string
		want string
	}{
		{`x == "42"`, "42"},
		{`x == "true"`, "true"},
		{`x == "1.5"`, "1.5"},
	}
	for _, tc := range cases {
		node, err := expr.Parse(tc.expr)
		if err != nil {
			t.Fatalf("%s: parse error: %v", tc.expr, err)
		}
		cmp := node.(*expr.CmpNode)
		got, ok := cmp.Value.(string)
		if !ok {
			t.Fatalf("%s: value type: got %T want string", tc.expr, cmp.Value)
		}
		if got != tc.want {
			t.Errorf("%s: value: got %q want %q", tc.expr, got, tc.want)
		}
	}
}

func TestEvalQuotedStringLiteral(t *testing.T) {
	// The docker provider's case: health_status != "unhealthy" evaluated
	// against a fact carrying the string "healthy" — should be true.
	node, _ := expr.Parse(`health_status != "unhealthy"`)
	ctx := makeCtx("container", map[string]map[string]any{
		"container": {"health_status": "healthy"},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error(`expected true ("healthy" != "unhealthy")`)
	}
}

func TestParseUnterminatedStringLiteral(t *testing.T) {
	if _, err := expr.Parse(`health_status != "unhealthy`); err == nil {
		t.Fatal("expected parse error for unterminated string literal")
	}
}

func TestParseEmptyStringLiteral(t *testing.T) {
	node, err := expr.Parse(`label == ""`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	cmp := node.(*expr.CmpNode)
	if cmp.Value != "" {
		t.Errorf("value: got %q want empty string", cmp.Value)
	}
}

func TestParseStringLiteralWithSpaces(t *testing.T) {
	// The main reason to quote: string values containing characters the
	// bareword tokenizer can't represent (here, a space).
	node, err := expr.Parse(`phase == "rolling update"`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	cmp := node.(*expr.CmpNode)
	if cmp.Value != "rolling update" {
		t.Errorf("value: got %q want %q", cmp.Value, "rolling update")
	}
}

func TestEvalQuotedStringLiteralFalse(t *testing.T) {
	// Same expression, fact value matches the literal — should be false.
	node, _ := expr.Parse(`health_status != "unhealthy"`)
	ctx := makeCtx("container", map[string]map[string]any{
		"container": {"health_status": "unhealthy"},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if result {
		t.Error(`expected false ("unhealthy" != "unhealthy" is false)`)
	}
}

func TestEvalCurrentComponentImplicit(t *testing.T) {
	// A bare ref (no component prefix) uses ctx.CurrentComponent
	node, _ := expr.Parse("ready_replicas == 3")
	ctx := makeCtx("db", map[string]map[string]any{
		"db": {"ready_replicas": 3},
	}, nil)
	result, err := node.Eval(ctx)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if !result {
		t.Error("expected true using implicit current component")
	}
}
