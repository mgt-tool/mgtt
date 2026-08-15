// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import "testing"

// Root cause must equal. No relaxation here.
func TestMatches_RootCauseStrict(t *testing.T) {
	if matches(Expectation{RootCause: "rds"}, Expectation{RootCause: "redis"}) {
		t.Error("different root causes must not match")
	}
	if !matches(Expectation{RootCause: "rds"}, Expectation{RootCause: "rds"}) {
		t.Error("same root cause must match")
	}
}

// Path as an ordered subsequence: expected must appear in actual in
// order, extras between allowed.
func TestMatches_PathSubsequenceAllowsExtras(t *testing.T) {
	exp := Expectation{RootCause: "rds", Path: []string{"nginx", "api", "rds"}}
	// Catalog source added `legacy-gateway` between api and rds — the
	// root cause didn't change, the scenario should still pass.
	act := Expectation{RootCause: "rds", Path: []string{"nginx", "api", "legacy-gateway", "rds"}}
	if !matches(exp, act) {
		t.Error("ordered subsequence should match when actual has extras between expected elements")
	}
}

// Exact match on Path still works (over-specified scenarios don't
// regress under the relaxed matcher).
func TestMatches_PathExactStillPasses(t *testing.T) {
	exp := Expectation{RootCause: "rds", Path: []string{"nginx", "api", "rds"}}
	act := Expectation{RootCause: "rds", Path: []string{"nginx", "api", "rds"}}
	if !matches(exp, act) {
		t.Error("exact path match should pass")
	}
}

// Order in Path still matters — rds→api is not the same as api→rds
// for chain semantics.
func TestMatches_PathRespectsOrder(t *testing.T) {
	exp := Expectation{RootCause: "rds", Path: []string{"api", "rds"}}
	act := Expectation{RootCause: "rds", Path: []string{"rds", "api"}}
	if matches(exp, act) {
		t.Error("out-of-order path should not match")
	}
}

// Missing an expected element in actual fails — scenario loses its
// guard.
func TestMatches_PathMissingElementFails(t *testing.T) {
	exp := Expectation{RootCause: "rds", Path: []string{"nginx", "api", "rds"}}
	act := Expectation{RootCause: "rds", Path: []string{"nginx", "rds"}} // missing api
	if matches(exp, act) {
		t.Error("missing expected element should not match (under-specification would mask a bug)")
	}
}

// Empty expected Path = no assertion, any actual passes.
func TestMatches_EmptyPathAssertsNothing(t *testing.T) {
	exp := Expectation{RootCause: "rds"}
	act := Expectation{RootCause: "rds", Path: []string{"nginx", "api", "rds"}}
	if !matches(exp, act) {
		t.Error("empty expected path must not constrain actual")
	}
}

// Eliminated is a subset — adding topology-only nodes to the model
// shouldn't cascade-break every scenario.
func TestMatches_EliminatedSubsetAllowsExtras(t *testing.T) {
	exp := Expectation{RootCause: "rds", Eliminated: []string{"frontend", "redis"}}
	// Catalog source imported `payment-gateway` and `observability-collector`
	// as topology-only components. They're now in actual.Eliminated but
	// weren't known when the scenario was written.
	act := Expectation{RootCause: "rds", Eliminated: []string{"frontend", "redis", "payment-gateway", "observability-collector"}}
	if !matches(exp, act) {
		t.Error("subset eliminated should match when actual has extras")
	}
}

// Missing an expected Eliminated entry fails — the scenario was
// explicit that component X had to be ruled out, and it wasn't.
func TestMatches_EliminatedMissingEntryFails(t *testing.T) {
	exp := Expectation{RootCause: "rds", Eliminated: []string{"frontend", "redis"}}
	act := Expectation{RootCause: "rds", Eliminated: []string{"frontend"}} // missing redis
	if matches(exp, act) {
		t.Error("missing expected eliminated entry should not match")
	}
}

// Helpers are worth testing directly — edge cases that don't surface
// through matches() alone.
func TestIsOrderedSubsequence(t *testing.T) {
	cases := []struct {
		sub, seq []string
		want     bool
	}{
		{nil, nil, true},
		{nil, []string{"a"}, true},
		{[]string{"a"}, nil, false},
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a", "c"}, []string{"a", "b", "c"}, true},
		{[]string{"c", "a"}, []string{"a", "b", "c"}, false},
		{[]string{"a", "a"}, []string{"a", "b", "c"}, false},
		{[]string{"a", "a"}, []string{"a", "a", "b"}, true},
	}
	for _, c := range cases {
		if got := isOrderedSubsequence(c.sub, c.seq); got != c.want {
			t.Errorf("isOrderedSubsequence(%v, %v) = %v, want %v", c.sub, c.seq, got, c.want)
		}
	}
}

func TestIsSubset(t *testing.T) {
	cases := []struct {
		sub, super []string
		want       bool
	}{
		{nil, nil, true},
		{nil, []string{"a"}, true},
		{[]string{"a"}, nil, false},
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a", "b"}, []string{"a", "b", "c"}, true},
		{[]string{"a", "b"}, []string{"a", "c"}, false}, // missing b
	}
	for _, c := range cases {
		if got := isSubset(c.sub, c.super); got != c.want {
			t.Errorf("isSubset(%v, %v) = %v, want %v", c.sub, c.super, got, c.want)
		}
	}
}
