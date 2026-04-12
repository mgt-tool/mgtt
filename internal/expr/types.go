package expr

import "github.com/sajonaro/mgtt/internal/facts"

// CmpOp is a comparison operator.
type CmpOp int

const (
	OpEq  CmpOp = iota // ==
	OpNeq              // !=
	OpLt               // <
	OpGt               // >
	OpLte              // <=
	OpGte              // >=
)

// Value holds a parsed literal. Exactly one field is non-nil.
type Value struct {
	IntVal    *int
	FloatVal  *float64
	BoolVal   *bool
	StringVal *string
}

// Node is a node in the expression AST.
type Node interface {
	Eval(ctx Ctx) (bool, error)
}

// AndNode evaluates L && R with UnresolvedError semantics.
type AndNode struct{ L, R Node }

// OrNode evaluates L || R with UnresolvedError semantics.
type OrNode struct{ L, R Node }

// CmpNode compares a fact (or state) against a literal value.
type CmpNode struct {
	Component string // empty = use Ctx.CurrentComponent
	Fact      string // "state" means look in ctx.States
	Op        CmpOp
	Value     Value
}

// Ctx provides runtime context for expression evaluation.
type Ctx struct {
	CurrentComponent string
	Facts            *facts.Store
	States           map[string]string
}
