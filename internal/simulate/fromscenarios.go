// Package simulate — fromscenarios: iterate every enumerated Scenario
// and assert the occam strategy converges on its declared root.
//
// Approach: synthesize facts that make each step of the chain evaluate
// true under its type's state.When predicate, seed an in-memory fact
// store, and then run Occam repeatedly. The strategy should converge
// (Done) with the root matching the scenario's declared root. When
// Occam asks for more probes, we satisfy them on the fly from the same
// scenario chain.
//
// Predicate-to-fact synthesis: the walker handles the canonical shapes
// that `mgtt model validate` accepts on disk — equality/inequality,
// numeric comparisons against literals or against another fact-name on
// the RHS, and AND/OR composition (OR prefers the branch whose
// bindings don't conflict with the synthesis-in-progress map).
//
// Unsupported: `state == "..."` clauses that reference another
// component's derived state cannot be satisfied by writing a fact —
// the synthesizer surfaces these as an explicit error so the caller
// can distinguish synthesizer limitations from strategy bugs.
package simulate

import (
	"errors"
	"fmt"

	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// ErrCrossStateRef signals the synthesizer hit a `state == "..."` or
// `<component>.state == "..."` shape. It can't satisfy such a
// predicate by writing a fact (the state is derived downstream). The
// caller surfaces this as a FAIL with an explicit "unsupported
// predicate shape" note rather than a mysterious Occam-Stuck result.
var ErrCrossStateRef = errors.New("synthesize: unsupported predicate shape: cross-state reference on component.state")

// RunFromScenarios iterates every scenario and runs the occam strategy
// in a loop to completion (Done or Stuck), asserting the root matches.
// Returns (passed, failed, details). details is per-scenario result
// lines for the caller to print.
func RunFromScenarios(m *model.Model, reg *providersupport.Registry, scs []scenarios.Scenario) (passed, failed int, details []string) {
	for _, s := range scs {
		ok, detail := runOneScenario(m, reg, scs, s)
		if ok {
			passed++
		} else {
			failed++
		}
		details = append(details, detail)
	}
	return
}

func runOneScenario(m *model.Model, reg *providersupport.Registry, all []scenarios.Scenario, s scenarios.Scenario) (bool, string) {
	store := facts.NewInMemory()
	if err := synthesizeFactsForScenario(store, m, reg, s); err != nil {
		if errors.Is(err, ErrCrossStateRef) {
			return false, fmt.Sprintf("%s: FAIL — %v", s.ID, err)
		}
		return false, fmt.Sprintf("%s: FAIL — synthesize facts: %v", s.ID, err)
	}
	// For each component not on the target chain, pin it to its
	// default-active state so competing scenarios whose chains include
	// that component get eliminated (their non-default state predicate
	// is contradicted by the active-state facts).
	synthesizeActiveStatesForNonChain(store, m, reg, s)

	in := strategy.Input{Model: m, Registry: reg, Store: store, Scenarios: all}
	for i := 0; i < 50; i++ {
		d := strategy.Occam().SuggestProbe(in)
		if d.Done {
			if d.RootCause == nil ||
				d.RootCause.Root.Component != s.Root.Component ||
				d.RootCause.Root.State != s.Root.State {
				got := "nil"
				if d.RootCause != nil {
					got = fmt.Sprintf("%s.%s", d.RootCause.Root.Component, d.RootCause.Root.State)
				}
				return false, fmt.Sprintf("%s: FAIL — want root %s.%s; got %s",
					s.ID, s.Root.Component, s.Root.State, got)
			}
			return true, fmt.Sprintf("%s: PASS", s.ID)
		}
		if d.Stuck {
			return false, fmt.Sprintf("%s: FAIL — Stuck: %s", s.ID, d.Reason)
		}
		if d.Probe == nil {
			return false, fmt.Sprintf("%s: FAIL — no probe, no done/stuck", s.ID)
		}
		// Strategy wants more probes — answer based on scenario intent.
		if err := synthesizeProbeAnswer(store, m, reg, s, d.Probe); err != nil {
			return false, fmt.Sprintf("%s: FAIL — synthesize probe answer: %v", s.ID, err)
		}
	}
	return false, fmt.Sprintf("%s: FAIL — did not converge in 50 iterations", s.ID)
}

// synthesizeFactsForScenario walks the scenario chain and, for each
// step, derives fact values that make its state predicate evaluate true
// and writes them into store.
func synthesizeFactsForScenario(store *facts.Store, m *model.Model, reg *providersupport.Registry, s scenarios.Scenario) error {
	return synthesizeFactsForSteps(store, m, reg, s.Chain)
}

// synthesizeFactsForSteps is the chain-prefix variant used by --fuzz.
// Non-fatal synthesis errors are swallowed — occam falls back to probe
// requests we answer on demand. ErrCrossStateRef, however, bubbles up:
// no probe answer can rescue a predicate that keys off another
// component's derived state.
func synthesizeFactsForSteps(store *facts.Store, m *model.Model, reg *providersupport.Registry, steps []scenarios.Step) error {
	for _, step := range steps {
		if err := synthesizeStep(store, m, reg, step); err != nil {
			if errors.Is(err, ErrCrossStateRef) {
				return err
			}
			// Other errors are not fatal — the occam strategy will fall
			// back to asking probes and we'll answer them on demand.
			continue
		}
	}
	return nil
}

// synthesizeStep finds the StateDef that matches step.State and derives
// fact values that satisfy its When predicate.
func synthesizeStep(store *facts.Store, m *model.Model, reg *providersupport.Registry, step scenarios.Step) error {
	t, err := resolveStepType(m, reg, step.Component)
	if err != nil {
		return err
	}
	for _, st := range t.States {
		if st.Name != step.State {
			continue
		}
		if st.When == nil {
			return nil
		}
		assignments, err := deriveSatisfyingAssignments(st.When, true, t.Facts)
		if err != nil {
			return err
		}
		for k, v := range assignments {
			store.Append(step.Component, facts.Fact{
				Key:       k,
				Value:     v,
				Collector: "simulate-synth",
			})
		}
		return nil
	}
	return nil
}

// synthesizeProbeAnswer answers an Occam-requested probe on (component,
// fact). When the component is in the scenario's chain we satisfy the
// predicate of its step-state. Otherwise we write a benign "active"
// value — a placeholder that lets occam treat that component as
// verified-healthy and move on.
func synthesizeProbeAnswer(store *facts.Store, m *model.Model, reg *providersupport.Registry, s scenarios.Scenario, p *strategy.Probe) error {
	for _, step := range s.Chain {
		if step.Component == p.Component {
			return synthesizeStep(store, m, reg, step)
		}
	}
	// Component not on the scenario — seed a harmless fact so the
	// strategy stops re-asking. The exact value doesn't matter; what
	// matters is that FactsFor(component) returns non-empty so pickSym
	// skips this component on the next pass.
	store.Append(p.Component, facts.Fact{
		Key:       p.Fact,
		Value:     "synth-default",
		Collector: "simulate-synth",
	})
	return nil
}

// synthesizeActiveStatesForNonChain writes facts for every component
// not on s's chain that satisfy the type's DefaultActiveState predicate.
// This removes ambiguity when multiple enumerated scenarios touch
// overlapping components — the active-state facts contradict any
// scenario that includes this component in a non-default (failing)
// state.
func synthesizeActiveStatesForNonChain(store *facts.Store, m *model.Model, reg *providersupport.Registry, s scenarios.Scenario) {
	onChain := map[string]bool{}
	for _, step := range s.Chain {
		onChain[step.Component] = true
	}
	for compName := range m.Components {
		if onChain[compName] {
			continue
		}
		t, err := resolveStepType(m, reg, compName)
		if err != nil || t == nil || t.DefaultActiveState == "" {
			continue
		}
		for _, st := range t.States {
			if st.Name != t.DefaultActiveState {
				continue
			}
			if st.When == nil {
				continue
			}
			// Active-state seeding is best-effort. A cross-state ref in
			// another component's default-active predicate shouldn't fail
			// the whole scenario — we just skip that component.
			assignments, err := deriveSatisfyingAssignments(st.When, true, t.Facts)
			if err != nil {
				break
			}
			for k, v := range assignments {
				store.Append(compName, facts.Fact{
					Key:       k,
					Value:     v,
					Collector: "simulate-synth-active",
				})
			}
			break
		}
	}
}

func resolveStepType(m *model.Model, reg *providersupport.Registry, compName string) (*providersupport.Type, error) {
	comp := m.Components[compName]
	if comp == nil {
		return nil, fmt.Errorf("component %q not in model", compName)
	}
	providers := comp.Providers
	if len(providers) == 0 {
		providers = m.Meta.Providers
	}
	t, _, err := reg.ResolveType(providers, comp.Type)
	if err != nil {
		return nil, fmt.Errorf("resolve type for %q: %w", compName, err)
	}
	if t == nil {
		return nil, fmt.Errorf("type %q unresolved for component %q", comp.Type, compName)
	}
	return t, nil
}

// deriveSatisfyingAssignments walks the compiled expression AST and
// returns fact-key→value assignments that make it evaluate to want.
//
// knownFacts lets the walker distinguish a fact-reference RHS like
// `ready_replicas < desired_replicas` (where `desired_replicas` is a
// string token that matches a fact name in the same type) from a
// plain string literal compared via equality. When RHS is a fact
// reference the walker picks an integer pair that satisfies the
// comparison and binds both sides.
//
// Handles:
//   - CmpNode with int/float/bool/string literal on the RHS
//   - CmpNode with a fact-name RHS (ready_replicas < desired_replicas)
//   - AndNode: recurse both sides with the same `want`
//   - OrNode: prefers whichever branch's bindings don't conflict with
//     the synthesis-in-progress map; if both conflict, left wins
//
// Returns ErrCrossStateRef when any CmpNode's LHS keys off
// component.state (a derived-state reference the synthesizer cannot
// satisfy by writing a fact).
func deriveSatisfyingAssignments(node expr.Node, want bool, knownFacts map[string]*providersupport.FactSpec) (map[string]any, error) {
	out := map[string]any{}
	if err := assignFromNode(node, want, out, knownFacts); err != nil {
		return nil, err
	}
	return out, nil
}

func assignFromNode(node expr.Node, want bool, out map[string]any, knownFacts map[string]*providersupport.FactSpec) error {
	switch n := node.(type) {
	case expr.CmpNode:
		return assignFromCmp(n, want, out, knownFacts)
	case expr.AndNode:
		if err := assignFromNode(n.L, want, out, knownFacts); err != nil {
			return err
		}
		return assignFromNode(n.R, want, out, knownFacts)
	case expr.OrNode:
		// Rebalance: try both branches on fresh scratch maps and pick
		// whichever doesn't conflict with bindings already in `out`. If
		// both branches conflict (or neither binds anything), fall back
		// to the left branch — the existing-test behavior.
		leftScratch := map[string]any{}
		leftErr := assignFromNode(n.L, want, leftScratch, knownFacts)
		if leftErr != nil && !errors.Is(leftErr, ErrCrossStateRef) {
			leftErr = nil
		}
		rightScratch := map[string]any{}
		rightErr := assignFromNode(n.R, want, rightScratch, knownFacts)
		if rightErr != nil && !errors.Is(rightErr, ErrCrossStateRef) {
			rightErr = nil
		}
		// If a branch is cross-state-ref and the other is clean, prefer
		// the clean one without failing.
		leftCross := errors.Is(leftErr, ErrCrossStateRef)
		rightCross := errors.Is(rightErr, ErrCrossStateRef)
		leftOK := !leftCross && len(leftScratch) > 0
		rightOK := !rightCross && len(rightScratch) > 0
		leftConflict := leftOK && bindingsConflict(leftScratch, out)
		rightConflict := rightOK && bindingsConflict(rightScratch, out)

		switch {
		case leftOK && !leftConflict:
			mergeInto(out, leftScratch)
			return nil
		case rightOK && !rightConflict:
			mergeInto(out, rightScratch)
			return nil
		case leftOK:
			mergeInto(out, leftScratch)
			return nil
		case rightOK:
			mergeInto(out, rightScratch)
			return nil
		case leftCross && rightCross:
			return ErrCrossStateRef
		}
		return nil
	case *expr.CmpNode:
		return assignFromNode(*n, want, out, knownFacts)
	case *expr.AndNode:
		return assignFromNode(*n, want, out, knownFacts)
	case *expr.OrNode:
		return assignFromNode(*n, want, out, knownFacts)
	}
	return nil
}

// assignFromCmp handles a single CmpNode. Binds facts from both LHS
// and (when fact-referenced) RHS. Returns ErrCrossStateRef when LHS
// references a derived state.
func assignFromCmp(n expr.CmpNode, want bool, out map[string]any, knownFacts map[string]*providersupport.FactSpec) error {
	if n.Fact == "" {
		return nil
	}
	if n.Fact == "state" {
		return ErrCrossStateRef
	}

	// 1a. Fact-on-RHS: detect when n.Value is a string that names
	// another fact in the same type. Pick an integer pair satisfying
	// the comparison and bind both.
	if rhsName, isFactRef := factRefValue(n.Value, knownFacts, n.Fact); isFactRef {
		lv, rv, ok := satisfyCmpBothFactRefs(n.Op, want)
		if !ok {
			return nil
		}
		if _, exists := out[n.Fact]; !exists {
			out[n.Fact] = lv
		}
		if _, exists := out[rhsName]; !exists {
			out[rhsName] = rv
		}
		return nil
	}

	v, ok := satisfyCmp(n, want)
	if !ok {
		return nil
	}
	// Don't overwrite a prior binding for the same fact — first
	// clause wins. A later conflicting clause just means the
	// predicate is unsatisfiable and our synthesizer cannot help
	// with this scenario; that's reported upstream as a "Stuck".
	if _, exists := out[n.Fact]; !exists {
		out[n.Fact] = v
	}
	return nil
}

// factRefValue returns (name, true) when v is a string that matches a
// fact name in knownFacts (excluding selfFact, which is the LHS and
// would compare a fact against itself — treat that as a literal).
func factRefValue(v any, knownFacts map[string]*providersupport.FactSpec, selfFact string) (string, bool) {
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	if s == selfFact {
		return "", false
	}
	if _, ok := knownFacts[s]; !ok {
		return "", false
	}
	return s, true
}

// satisfyCmpBothFactRefs returns an (lhs, rhs) integer pair such that
// lhs <op> rhs evaluates to want. The scenario's step binds the LHS to
// the state the step declares; peer facts can take any satisfying
// value.
func satisfyCmpBothFactRefs(op expr.CmpOp, want bool) (any, any, bool) {
	effective := op
	if !want {
		switch op {
		case expr.OpEq:
			effective = expr.OpNeq
		case expr.OpNeq:
			effective = expr.OpEq
		case expr.OpLt:
			effective = expr.OpGte
		case expr.OpGt:
			effective = expr.OpLte
		case expr.OpLte:
			effective = expr.OpGt
		case expr.OpGte:
			effective = expr.OpLt
		}
	}
	switch effective {
	case expr.OpEq:
		return 1, 1, true
	case expr.OpNeq:
		return 0, 1, true
	case expr.OpLt:
		return 0, 1, true
	case expr.OpGt:
		return 1, 0, true
	case expr.OpLte:
		return 0, 1, true
	case expr.OpGte:
		return 1, 0, true
	}
	return nil, nil, false
}

// bindingsConflict reports whether any key in candidate is already
// bound in existing to a different value.
func bindingsConflict(candidate, existing map[string]any) bool {
	for k, cv := range candidate {
		if ev, ok := existing[k]; ok && ev != cv {
			return true
		}
	}
	return false
}

// mergeInto copies src into dst without overwriting existing bindings.
func mergeInto(dst, src map[string]any) {
	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

// satisfyCmp returns a concrete fact value that, when compared against
// n.Value under n.Op, yields want.
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

func satisfyBool(op expr.CmpOp, target bool, want bool) (any, bool) {
	switch op {
	case expr.OpEq:
		if want {
			return target, true
		}
		return !target, true
	case expr.OpNeq:
		if want {
			return !target, true
		}
		return target, true
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
	// Flip operator semantics when want == false.
	effective := op
	if !want {
		switch op {
		case expr.OpEq:
			effective = expr.OpNeq
		case expr.OpNeq:
			effective = expr.OpEq
		case expr.OpLt:
			effective = expr.OpGte
		case expr.OpGt:
			effective = expr.OpLte
		case expr.OpLte:
			effective = expr.OpGt
		case expr.OpGte:
			effective = expr.OpLt
		}
	}
	switch effective {
	case expr.OpEq:
		return mk(target), true
	case expr.OpNeq:
		return mk(target + 1), true
	case expr.OpLt:
		return mk(target - 1), true
	case expr.OpGt:
		return mk(target + 1), true
	case expr.OpLte:
		return mk(target), true
	case expr.OpGte:
		return mk(target), true
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
