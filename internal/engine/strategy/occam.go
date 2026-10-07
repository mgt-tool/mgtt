// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"sort"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

type occamStrategy struct{}

// Occam returns the shortest-scenario-first strategy.
func Occam() Strategy { return occamStrategy{} }

func (occamStrategy) Name() string { return "occam" }

func (occamStrategy) SuggestProbe(in Input) Decision {
	if in.Graph != nil && len(in.Scenarios) == 0 {
		if d, ok := occamOnGraph(in); ok {
			return d
		}
		in.Scenarios = scenarios.Expand(in.Graph) // a cycle: count by listing
	}
	live := FilterLive(in.Scenarios, in.Store, in.Model, in.Registry)

	switch len(live) {
	case 0:
		return Decision{Stuck: true, Reason: "no scenario matches observed facts"}
	case 1:
		return Decision{Done: true, RootCause: &live[0], Reason: "single scenario remains"}
	}

	// Rank keys are computed once per scenario. A scenario's elimination
	// count — how many OTHER live scenarios touch the component its next
	// probe targets — is that component's touch count minus itself, so
	// one pass over the live set prices every candidate: O(n·L), not the
	// O(n² log n) of recounting inside the comparator.
	touches := liveTouchCounts(live)
	type ranked struct {
		s       scenarios.Scenario
		suspect bool
		elim    int
	}
	rs := make([]ranked, len(live))
	for i, s := range live {
		rs[i] = ranked{s: s, suspect: touchesAnySuspect(s, in.Suspects)}
		if p := pickSymptomInward(s, in.Store, in.Model, in.Registry); p != nil {
			rs[i].elim = touches[p.Component] - 1
		}
	}
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].s.Length() != rs[j].s.Length() {
			return rs[i].s.Length() < rs[j].s.Length()
		}
		if rs[i].suspect != rs[j].suspect {
			return rs[i].suspect // true comes first
		}
		if rs[i].elim != rs[j].elim {
			return rs[i].elim > rs[j].elim
		}
		return scenarios.Less(rs[i].s, rs[j].s)
	})
	for i := range rs {
		live[i] = rs[i].s
	}

	chosen := live[0]
	probe := pickSymptomInward(chosen, in.Store, in.Model, in.Registry)
	if probe == nil {
		return Decision{Stuck: true, Reason: "chosen scenario fully verified but live set has multiple"}
	}
	for _, s := range live {
		for _, step := range s.Chain {
			if step.Component == probe.Component {
				probe.Eliminates = append(probe.Eliminates, s.ID)
				break
			}
		}
	}
	probe.EliminatesCount = len(probe.Eliminates)
	return Decision{Probe: probe}
}

func touchesAnySuspect(s scenarios.Scenario, hints []SuspectHint) bool {
	for _, h := range hints {
		if s.TouchesComponent(h.Component) {
			if h.State == "" {
				return true
			}
			for _, step := range s.Chain {
				if step.Component == h.Component && step.State == h.State {
					return true
				}
			}
		}
	}
	return false
}

// liveTouchCounts maps each component to the number of live scenarios
// whose chain touches it (once per scenario, however many steps).
func liveTouchCounts(live []scenarios.Scenario) map[string]int {
	counts := map[string]int{}
	for _, s := range live {
		seen := map[string]bool{}
		for _, step := range s.Chain {
			if !seen[step.Component] {
				seen[step.Component] = true
				counts[step.Component]++
			}
		}
	}
	return counts
}

// pickSymptomInward walks the chain terminal→root and returns a probe for
// the first step that is not yet truly verified at the fact level. A step
// is verified iff every fact it directly relies on — step.Observes for a
// terminal step, or every fact referenced in state.When for a non-terminal
// step — is already in the store. The prior component-level gate (any fact
// collected → skip) was too coarse.
func pickSymptomInward(s scenarios.Scenario, store *facts.Store, m *model.Model, reg *providersupport.Registry) *Probe {
	if m == nil || reg == nil {
		return nil
	}
	// Inward: walk the chain from terminal (symptom) toward root.
	for i := len(s.Chain) - 1; i >= 0; i-- {
		step := s.Chain[i]
		if probe := probeForStep(step, store, m, reg); probe != nil {
			return probe
		}
	}
	return nil
}

// probeForStep returns the next probe needed to verify step, or nil
// when every fact step depends on is already collected (or the step's
// component/type is unavailable).
func probeForStep(step scenarios.Step, store *facts.Store, m *model.Model, reg *providersupport.Registry) *Probe {
	comp := m.Components[step.Component]
	if comp == nil {
		return nil
	}
	t, providerName, err := comp.ResolveType(m, reg)
	if err != nil || t == nil {
		return nil
	}
	factName := nextFactToProbe(step, t, store)
	if factName == "" {
		return nil
	}
	fs := t.Facts[factName]
	if fs == nil {
		return nil
	}
	return &Probe{
		Component: step.Component,
		Fact:      factName,
		Provider:  providerName,
		Type:      t.Name,
		Resource:  comp.ResourceName(),
		Cost:      fs.Probe.Cost,
		Access:    fs.Probe.Access,
		Command:   fs.Probe.Cmd,
		ParseMode: fs.Probe.Parse,
		Window:    fs.Window,
		Derive:    fs.Derive,
		Vars:      mergeVars(m.Meta.Vars, comp.Vars),
	}
}

// nextFactToProbe picks the fact name step most wants verified next:
// the first in step's observing-facts list (or all type facts, as a
// fallback) that the store hasn't seen yet. Returns "" when step is
// fully verified.
func nextFactToProbe(step scenarios.Step, t *providersupport.Type, store *facts.Store) string {
	needed := validStepFacts(step, t)
	if len(needed) == 0 {
		needed = sortedFactNames(t.Facts)
	}
	for _, n := range needed {
		if store == nil || store.Latest(step.Component, n) == nil {
			return n
		}
	}
	return ""
}

// validStepFacts returns step.Observes (or state.When's fact refs)
// filtered to names that actually exist on t. The type definition is
// the authority — collectFactRefs is permissive, so we drop any
// string literal that isn't a real fact.
func validStepFacts(step scenarios.Step, t *providersupport.Type) []string {
	names := stepObservingFacts(step, t)
	filtered := names[:0]
	for _, n := range names {
		if _, ok := t.Facts[n]; ok {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

// sortedFactNames returns every fact name on t in alphabetical order.
// Fallback when a step has no statically-derivable fact dependencies.
func sortedFactNames(facts map[string]*providersupport.FactSpec) []string {
	out := make([]string, 0, len(facts))
	for n := range facts {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// stepObservingFacts returns the facts a step directly depends on:
//   - For a terminal step (Observes non-empty), the observed facts.
//   - For a non-terminal step, the facts referenced by the state's When
//     predicate (in deterministic alphabetical order).
func stepObservingFacts(step scenarios.Step, t *providersupport.Type) []string {
	if step.IsTerminal() {
		out := append([]string(nil), step.Observes...)
		sort.Strings(out)
		return out
	}
	for _, st := range t.States {
		if st.Name != step.State {
			continue
		}
		return collectFactRefs(st.When)
	}
	return nil
}

// collectFactRefs walks a compiled when-predicate and returns the set of
// fact names it references on this component. The common case is a boolean
// AST of CmpNode / AndNode / OrNode — see internal/expr/types.go. Results
// are sorted for deterministic probe selection.
func collectFactRefs(node expr.Node) []string {
	seen := map[string]bool{}
	walkFactRefs(node, seen)
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// walkFactRefs recurses into And/Or/Cmp nodes to collect every fact
// name referenced by the expression. The parser only emits pointer
// variants; value-variant cases would be unreachable and were removed.
func walkFactRefs(node expr.Node, seen map[string]bool) {
	switch n := node.(type) {
	case *expr.AndNode:
		walkFactRefs(n.L, seen)
		walkFactRefs(n.R, seen)
	case *expr.OrNode:
		walkFactRefs(n.L, seen)
		walkFactRefs(n.R, seen)
	case *expr.CmpNode:
		recordCmpFacts(*n, seen)
	}
}

func recordCmpFacts(n expr.CmpNode, seen map[string]bool) {
	// LHS: record unless it's a cross-component state reference
	// (Fact=="state") — that doesn't name a fact on this type.
	// Same-component refs have n.Component=="" per parser.
	if n.Fact != "" && n.Fact != "state" && n.Component == "" {
		seen[n.Fact] = true
	}
	// RHS: a string literal that doesn't parse as a number is treated by
	// the evaluator as a fact reference on the same component (see
	// compareFactValue). Mirror that here so the probe can collect it.
	if s, ok := n.Value.(string); ok {
		if _, boolish := map[string]bool{"true": true, "false": true}[s]; boolish {
			return
		}
		// parser.InferValue leaves non-numeric, non-boolean strings as
		// strings; a literal that parsed as int/float would not arrive
		// here. The evaluator then treats it as a fact ref.
		seen[s] = true
	}
}
