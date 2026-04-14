package simulate

import (
	"sort"
	"time"

	"github.com/mgt-tool/mgtt/internal/engine"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// Run executes a single simulation scenario against the model and returns
// the result, including whether the scenario's expectations were met.
func Run(m *model.Model, reg *providersupport.Registry, sc *Scenario) *Result {
	store := facts.NewInMemory()
	for comp, kvs := range sc.Inject {
		for k, v := range kvs {
			store.Append(comp, facts.Fact{
				Key:       k,
				Value:     v,
				Collector: "simulate",
				At:        time.Now(),
			})
		}
	}

	tree := engine.Plan(m, reg, store, "")
	actual := extractConclusion(tree)
	pass := matches(sc.Expect, actual)

	return &Result{
		Scenario: sc,
		Actual:   actual,
		Pass:     pass,
	}
}

// extractConclusion derives the actual Expectation from a PathTree.
func extractConclusion(tree *engine.PathTree) Expectation {
	var ex Expectation

	// Root cause
	ex.RootCause = tree.RootCause
	if ex.RootCause == "" {
		ex.RootCause = "none"
	}

	// Path: find the surviving path whose terminal component is the root cause.
	if tree.RootCause != "" {
		for _, p := range tree.Paths {
			last := p.Components[len(p.Components)-1]
			if last == tree.RootCause {
				ex.Path = p.Components
				break
			}
		}
	}

	// Eliminated: components that appear ONLY on eliminated paths and NOT on
	// any surviving path.
	surviving := map[string]bool{}
	for _, p := range tree.Paths {
		for _, c := range p.Components {
			surviving[c] = true
		}
	}
	seen := map[string]bool{}
	var elim []string
	for _, p := range tree.Eliminated {
		for _, c := range p.Components {
			if !surviving[c] && !seen[c] {
				seen[c] = true
				elim = append(elim, c)
			}
		}
	}
	sort.Strings(elim)
	ex.Eliminated = elim

	return ex
}

// matches compares expected vs actual expectations. It is order-insensitive
// for the Eliminated list.
func matches(expected, actual Expectation) bool {
	// Root cause
	expRC := expected.RootCause
	actRC := actual.RootCause
	if expRC != actRC {
		return false
	}

	// Path (order-sensitive)
	if len(expected.Path) > 0 {
		if len(expected.Path) != len(actual.Path) {
			return false
		}
		for i := range expected.Path {
			if expected.Path[i] != actual.Path[i] {
				return false
			}
		}
	}

	// Eliminated (order-insensitive)
	if len(expected.Eliminated) != len(actual.Eliminated) {
		return false
	}
	expSorted := make([]string, len(expected.Eliminated))
	copy(expSorted, expected.Eliminated)
	sort.Strings(expSorted)

	actSorted := make([]string, len(actual.Eliminated))
	copy(actSorted, actual.Eliminated)
	sort.Strings(actSorted)

	for i := range expSorted {
		if expSorted[i] != actSorted[i] {
			return false
		}
	}

	return true
}
