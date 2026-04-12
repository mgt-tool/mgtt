package expr

import "fmt"

// UnresolvedError is returned when a fact or state cannot be resolved during
// evaluation. The engine treats (false, *UnresolvedError) as "unknown" — the
// path stays alive for later re-evaluation — rather than as a definitive false.
type UnresolvedError struct {
	Component string
	Fact      string
	Reason    string // "missing", "stale", "type mismatch"
}

func (e *UnresolvedError) Error() string {
	return fmt.Sprintf("unresolved %s.%s: %s", e.Component, e.Fact, e.Reason)
}
