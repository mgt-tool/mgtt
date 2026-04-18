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
// Predicate-to-fact synthesis limitations: the walker handles the
// canonical shapes that `mgtt model validate` accepts on disk —
// equality/inequality, numeric comparisons against literals, and
// AND/OR composition. Cross-fact references (`ready_replicas <
// desired_replicas`) and `state ==` clauses fall through to "no value
// synthesized" and will cause a scenario whose state predicate depends
// on them to be reported as a synthesizer limitation rather than a
// strategy bug.
package simulate

import (
	"fmt"

	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

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
func synthesizeFactsForSteps(store *facts.Store, m *model.Model, reg *providersupport.Registry, steps []scenarios.Step) error {
	for _, step := range steps {
		if err := synthesizeStep(store, m, reg, step); err != nil {
			// A single step we can't synthesize for is not fatal — the
			// occam strategy will fall back to asking probes and we'll
			// answer them on demand.
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
		assignments := deriveSatisfyingAssignments(st.When, true)
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
			for k, v := range deriveSatisfyingAssignments(st.When, true) {
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
// Handles:
//   - CmpNode with int/float/bool/string literal on the RHS
//   - AndNode: recurse both sides with the same `want`
//   - OrNode: recurse left side only (one satisfier is enough)
//
// Skipped (documented limitations):
//   - CmpNode.Fact == "state" (cross-state reference, not a raw fact)
//   - CmpNode with a non-numeric string RHS that is actually a
//     fact-reference (e.g. `ready_replicas < desired_replicas`)
//   - Non-trivial nested And-of-Or shapes where Or branch doesn't bind
//     anything (the Or recursion picks the left arm unconditionally)
func deriveSatisfyingAssignments(node expr.Node, want bool) map[string]any {
	out := map[string]any{}
	assignFromNode(node, want, out)
	return out
}

func assignFromNode(node expr.Node, want bool, out map[string]any) {
	switch n := node.(type) {
	case expr.CmpNode:
		if n.Fact == "" || n.Fact == "state" {
			return
		}
		v, ok := satisfyCmp(n, want)
		if !ok {
			return
		}
		// Don't overwrite a prior binding for the same fact — first
		// clause wins. A later conflicting clause just means the
		// predicate is unsatisfiable and our synthesizer cannot help
		// with this scenario; that's reported upstream as a "Stuck".
		if _, exists := out[n.Fact]; !exists {
			out[n.Fact] = v
		}
	case expr.AndNode:
		assignFromNode(n.L, want, out)
		assignFromNode(n.R, want, out)
	case expr.OrNode:
		// One branch is enough to satisfy an OR. Try the left branch
		// first; if it didn't yield any binding, try the right.
		before := len(out)
		assignFromNode(n.L, want, out)
		if len(out) == before {
			assignFromNode(n.R, want, out)
		}
	case *expr.CmpNode:
		assignFromNode(*n, want, out)
	case *expr.AndNode:
		assignFromNode(*n, want, out)
	case *expr.OrNode:
		assignFromNode(*n, want, out)
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
