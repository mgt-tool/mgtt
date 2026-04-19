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
	live := FilterLive(in.Scenarios, in.Store, in.Model, in.Registry)

	switch len(live) {
	case 0:
		return Decision{Stuck: true, Reason: "no scenario matches observed facts"}
	case 1:
		return Decision{Done: true, RootCause: &live[0], Reason: "single scenario remains"}
	}

	sort.SliceStable(live, func(i, j int) bool {
		if live[i].Length() != live[j].Length() {
			return live[i].Length() < live[j].Length()
		}
		si := touchesAnySuspect(live[i], in.Suspects)
		sj := touchesAnySuspect(live[j], in.Suspects)
		if si != sj {
			return si // true comes first
		}
		ei := crossEliminationCount(live[i], live, in.Store, in.Model, in.Registry)
		ej := crossEliminationCount(live[j], live, in.Store, in.Model, in.Registry)
		if ei != ej {
			return ei > ej
		}
		return live[i].ID < live[j].ID
	})

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

func crossEliminationCount(s scenarios.Scenario, live []scenarios.Scenario, store *facts.Store, m *model.Model, reg *providersupport.Registry) int {
	probe := pickSymptomInward(s, store, m, reg)
	if probe == nil {
		return 0
	}
	n := 0
	for _, other := range live {
		if other.ID == s.ID {
			continue
		}
		for _, step := range other.Chain {
			if step.Component == probe.Component {
				n++
				break
			}
		}
	}
	return n
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
	for i := len(s.Chain) - 1; i >= 0; i-- {
		step := s.Chain[i]
		comp := m.Components[step.Component]
		if comp == nil {
			continue
		}
		providers := comp.Providers
		if len(providers) == 0 {
			providers = m.Meta.Providers
		}
		t, providerName, err := reg.ResolveType(providers, comp.Type)
		if err != nil || t == nil {
			continue
		}
		needed := stepObservingFacts(step, t)
		// Drop names that don't correspond to an actual fact on the
		// type — collectFactRefs is permissive (RHS strings may be
		// literals, not fact refs), so the type definition is the
		// authority on what can actually be probed.
		filtered := needed[:0]
		for _, n := range needed {
			if _, ok := t.Facts[n]; ok {
				filtered = append(filtered, n)
			}
		}
		needed = filtered
		if len(needed) == 0 {
			// Nothing defined to verify this step against — fall back to
			// the first alphabetical fact on the type (preserves prior
			// behaviour for types without predicates).
			names := make([]string, 0, len(t.Facts))
			for n := range t.Facts {
				names = append(names, n)
			}
			sort.Strings(names)
			if len(names) == 0 {
				continue
			}
			needed = names
		}
		// Pick the first needed fact not yet collected.
		var factName string
		for _, n := range needed {
			if store == nil || store.Latest(step.Component, n) == nil {
				factName = n
				break
			}
		}
		if factName == "" {
			// All facts this step depends on are already collected —
			// truly verified, move on.
			continue
		}
		fs := t.Facts[factName]
		if fs == nil {
			continue
		}
		return &Probe{
			Component: step.Component,
			Fact:      factName,
			Provider:  providerName,
			Type:      t.Name,
			Cost:      fs.Probe.Cost,
			Access:    fs.Probe.Access,
			Command:   fs.Probe.Cmd,
			ParseMode: fs.Probe.Parse,
			Vars:      m.Meta.Vars,
		}
	}
	return nil
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

func walkFactRefs(node expr.Node, seen map[string]bool) {
	switch n := node.(type) {
	case *expr.AndNode:
		walkFactRefs(n.L, seen)
		walkFactRefs(n.R, seen)
	case expr.AndNode:
		walkFactRefs(n.L, seen)
		walkFactRefs(n.R, seen)
	case *expr.OrNode:
		walkFactRefs(n.L, seen)
		walkFactRefs(n.R, seen)
	case expr.OrNode:
		walkFactRefs(n.L, seen)
		walkFactRefs(n.R, seen)
	case *expr.CmpNode:
		recordCmpFacts(*n, seen)
	case expr.CmpNode:
		recordCmpFacts(n, seen)
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
