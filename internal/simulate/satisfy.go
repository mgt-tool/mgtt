// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"github.com/mgt-tool/mgtt/internal/expr"
)

// satisfyCmp returns a concrete fact value that, when compared against
// n.Value under n.Op, yields want. Used by the fromscenarios synthesis
// walker to turn a CmpNode predicate into a binding on the LHS fact.
func satisfyCmp(n expr.CmpNode, want bool) (any, bool) {
	switch v := n.Value.(type) {
	case bool:
		return satisfyBool(n.Op, v, want)
	case int:
		return satisfyNumeric(n.Op, float64(v), want, true)
	case int64:
		return satisfyNumeric(n.Op, float64(v), want, true)
	case float32:
		return satisfyNumeric(n.Op, float64(v), want, false)
	case float64:
		return satisfyNumeric(n.Op, v, want, false)
	case string:
		return satisfyString(n.Op, v, want)
	}
	return nil, false
}

// satisfyCmpBothFactRefs returns an (lhs, rhs) integer pair such that
// lhs <op> rhs evaluates to want. Used when both sides of a CmpNode
// are fact references (e.g. ready_replicas < desired_replicas).
func satisfyCmpBothFactRefs(op expr.CmpOp, want bool) (any, any, bool) {
	switch flipIfNotWant(op, want) {
	case expr.OpEq:
		return 1, 1, true
	case expr.OpNeq, expr.OpLt, expr.OpLte:
		return 0, 1, true
	case expr.OpGt, expr.OpGte:
		return 1, 0, true
	}
	return nil, nil, false
}

func satisfyBool(op expr.CmpOp, target bool, want bool) (any, bool) {
	switch op {
	case expr.OpEq:
		return target == want, true
	case expr.OpNeq:
		return target != want, true
	}
	return nil, false
}

func satisfyNumeric(op expr.CmpOp, target float64, want bool, prefInt bool) (any, bool) {
	mk := func(f float64) any {
		if prefInt && f == float64(int64(f)) {
			return int(f)
		}
		return f
	}
	switch flipIfNotWant(op, want) {
	case expr.OpEq, expr.OpLte, expr.OpGte:
		return mk(target), true
	case expr.OpNeq, expr.OpGt:
		return mk(target + 1), true
	case expr.OpLt:
		return mk(target - 1), true
	}
	return nil, false
}

func satisfyString(op expr.CmpOp, target string, want bool) (any, bool) {
	switch op {
	case expr.OpEq:
		if want {
			return target, true
		}
		return target + "-neg", true
	case expr.OpNeq:
		if want {
			return target + "-neg", true
		}
		return target, true
	}
	return nil, false
}

// flipIfNotWant returns op when want is true; its semantic negation
// otherwise. Lets satisfy* work with a single-direction switch
// regardless of whether we're synthesising for a true or false outcome.
func flipIfNotWant(op expr.CmpOp, want bool) expr.CmpOp {
	if want {
		return op
	}
	switch op {
	case expr.OpEq:
		return expr.OpNeq
	case expr.OpNeq:
		return expr.OpEq
	case expr.OpLt:
		return expr.OpGte
	case expr.OpGt:
		return expr.OpLte
	case expr.OpLte:
		return expr.OpGt
	case expr.OpGte:
		return expr.OpLt
	}
	return op
}
