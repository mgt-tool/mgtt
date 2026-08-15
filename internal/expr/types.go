// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package expr

type CmpOp int

const (
	OpEq  CmpOp = iota // ==
	OpNeq              // !=
	OpLt               // <
	OpGt               // >
	OpLte              // <=
	OpGte              // >=
)

// FactLookup is the minimal fact-store interface the evaluator needs.
// It returns the latest value for (component, key) and whether it was found.
// Keeping this as an interface decouples expr from internal/facts.
type FactLookup interface {
	LookupValue(component, key string) (any, bool)
}

// Node is a node in the expression AST.
type Node interface {
	Eval(ctx Ctx) (bool, error)
}

// AndNode evaluates L && R with UnresolvedError semantics.
type AndNode struct{ L, R Node }

// OrNode evaluates L || R with UnresolvedError semantics.
type OrNode struct{ L, R Node }

// CmpNode compares a fact (or state) against a literal value. Value holds
// the parsed literal as one of: int, float64, bool, string.
type CmpNode struct {
	Component string // empty = use Ctx.CurrentComponent
	Fact      string // "state" means look in ctx.States
	Op        CmpOp
	Value     any
}

// Ctx provides runtime context for expression evaluation.
type Ctx struct {
	CurrentComponent string
	Facts            FactLookup
	States           map[string]string
}
