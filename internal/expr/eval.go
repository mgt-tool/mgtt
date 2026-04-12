package expr

import (
	"errors"
	"fmt"
	"strconv"
)

// ---------------------------------------------------------------------------
// AndNode
// ---------------------------------------------------------------------------

// Eval evaluates the AND expression with UnresolvedError semantics:
//
//   - (false, nil) from L → short-circuit false, don't evaluate R
//   - (false, *UnresolvedError) from L → still evaluate R:
//     If R is (false, nil)       → return (false, nil)   — definitely false
//     If R is (false, unresolved) → return L's unresolved
//     If R is (true, nil)        → return L's unresolved (can't determine yet)
//   - (true, nil) from L → return R's result
func (n *AndNode) Eval(ctx Ctx) (bool, error) {
	lv, le := n.L.Eval(ctx)

	if !lv && le == nil {
		// Definitely false — short-circuit.
		return false, nil
	}

	if !lv {
		// L is unresolved (false, *UnresolvedError).
		rv, re := n.R.Eval(ctx)
		if !rv && re == nil {
			// R is definitely false → whole AND is definitely false.
			return false, nil
		}
		// R is either true (nil) or unresolved → propagate L's error.
		return false, le
	}

	// L is true → result depends on R.
	return n.R.Eval(ctx)
}

// ---------------------------------------------------------------------------
// OrNode
// ---------------------------------------------------------------------------

// Eval evaluates the OR expression with UnresolvedError semantics:
//
//   - (true, nil) from L  → short-circuit true
//   - (false, nil) from L → evaluate R, return R's result
//   - unresolved from L   → evaluate R:
//     If R is (true, nil)  → return (true, nil)
//     Otherwise            → return L's unresolved
func (n *OrNode) Eval(ctx Ctx) (bool, error) {
	lv, le := n.L.Eval(ctx)

	if lv && le == nil {
		return true, nil
	}

	if le == nil {
		// L is (false, nil) — definitely false — evaluate R.
		return n.R.Eval(ctx)
	}

	// L is unresolved.
	rv, re := n.R.Eval(ctx)
	if rv && re == nil {
		return true, nil
	}
	// R is false or unresolved → propagate L's error.
	return false, le
}

// ---------------------------------------------------------------------------
// CmpNode
// ---------------------------------------------------------------------------

// Eval evaluates a single comparison against the fact store or state map.
func (n *CmpNode) Eval(ctx Ctx) (bool, error) {
	component := n.Component
	if component == "" {
		component = ctx.CurrentComponent
	}

	// State comparison.
	if n.Fact == "state" {
		stateVal, ok := ctx.States[component]
		if !ok {
			return false, &UnresolvedError{Component: component, Fact: "state", Reason: "missing"}
		}
		return compareStrings(n.Op, stateVal, n.Value)
	}

	// Fact comparison.
	fact := ctx.Facts.Latest(component, n.Fact)
	if fact == nil {
		return false, &UnresolvedError{Component: component, Fact: n.Fact, Reason: "missing"}
	}

	return compareFactValue(n.Op, fact.Value, n.Value, component, ctx)
}

// ---------------------------------------------------------------------------
// Comparison helpers
// ---------------------------------------------------------------------------

// compareFactValue compares a runtime fact value against the parsed literal.
// component and ctx are used to resolve RHS identifiers as fact references
// when the nodeVal is a non-numeric string and the LHS fact is numeric.
func compareFactValue(op CmpOp, factVal any, nodeVal Value, component string, ctx Ctx) (bool, error) {
	// Bool fact.
	if bv, ok := factVal.(bool); ok {
		nb, err := asBool(nodeVal)
		if err != nil {
			return false, &UnresolvedError{Component: component, Reason: "type mismatch"}
		}
		return compareBools(op, bv, nb)
	}

	// String fact.
	if sv, ok := factVal.(string); ok {
		ns, err := asString(nodeVal)
		if err != nil {
			return false, &UnresolvedError{Component: component, Reason: "type mismatch"}
		}
		return compareStrings(op, sv, Value{StringVal: &ns})
	}

	// Numeric fact (int or float).
	factFloat, ok := toFloat(factVal)
	if !ok {
		return false, fmt.Errorf("unsupported fact type %T", factVal)
	}

	// Numeric value — try coercing the node value to float.
	nodeFloat, err := nodeValToFloat(nodeVal)
	if err != nil {
		// If the RHS is an unquoted identifier, try resolving it as a fact in
		// the same component (e.g. "ready_replicas < desired_replicas").
		if nodeVal.StringVal != nil {
			rhsFact := ctx.Facts.Latest(component, *nodeVal.StringVal)
			if rhsFact == nil {
				return false, &UnresolvedError{Component: component, Fact: *nodeVal.StringVal, Reason: "missing"}
			}
			rhsFloat, ok := toFloat(rhsFact.Value)
			if !ok {
				return false, &UnresolvedError{Component: component, Fact: *nodeVal.StringVal, Reason: "type mismatch"}
			}
			return compareFloats(op, factFloat, rhsFloat), nil
		}
		return false, &UnresolvedError{Component: component, Reason: "type mismatch"}
	}

	return compareFloats(op, factFloat, nodeFloat), nil
}

// toFloat converts an int or float64 fact value to float64.
func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

// nodeValToFloat converts a parsed Value to float64, accepting int, float,
// or a string representation of a number.
func nodeValToFloat(v Value) (float64, error) {
	if v.IntVal != nil {
		return float64(*v.IntVal), nil
	}
	if v.FloatVal != nil {
		return *v.FloatVal, nil
	}
	if v.StringVal != nil {
		f, err := strconv.ParseFloat(*v.StringVal, 64)
		if err == nil {
			return f, nil
		}
		i, err := strconv.Atoi(*v.StringVal)
		if err == nil {
			return float64(i), nil
		}
	}
	return 0, errors.New("cannot convert value to number")
}

func compareFloats(op CmpOp, a, b float64) bool {
	switch op {
	case OpEq:
		return a == b
	case OpNeq:
		return a != b
	case OpLt:
		return a < b
	case OpGt:
		return a > b
	case OpLte:
		return a <= b
	case OpGte:
		return a >= b
	}
	return false
}

func compareBools(op CmpOp, a, b bool) (bool, error) {
	switch op {
	case OpEq:
		return a == b, nil
	case OpNeq:
		return a != b, nil
	default:
		return false, fmt.Errorf("operator %v not valid for bool", op)
	}
}

// compareStrings compares fact string against node value (which may be a
// StringVal or another type coerced to string).
func compareStrings(op CmpOp, factStr string, nodeVal Value) (bool, error) {
	var s string
	if nodeVal.StringVal != nil {
		s = *nodeVal.StringVal
	} else {
		ns, err := asString(nodeVal)
		if err != nil {
			return false, err
		}
		s = ns
	}
	switch op {
	case OpEq:
		return factStr == s, nil
	case OpNeq:
		return factStr != s, nil
	default:
		return false, fmt.Errorf("operator %v not valid for string", op)
	}
}

func asBool(v Value) (bool, error) {
	if v.BoolVal != nil {
		return *v.BoolVal, nil
	}
	if v.StringVal != nil {
		switch *v.StringVal {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, fmt.Errorf("value is not bool")
}

func asString(v Value) (string, error) {
	if v.StringVal != nil {
		return *v.StringVal, nil
	}
	if v.IntVal != nil {
		return strconv.Itoa(*v.IntVal), nil
	}
	if v.FloatVal != nil {
		return strconv.FormatFloat(*v.FloatVal, 'f', -1, 64), nil
	}
	if v.BoolVal != nil {
		if *v.BoolVal {
			return "true", nil
		}
		return "false", nil
	}
	return "", fmt.Errorf("value has no string representation")
}
