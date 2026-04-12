package render

import (
	"fmt"
	"io"
	"sort"

	"mgtt/internal/facts"
	"mgtt/internal/model"
	"mgtt/internal/state"
)

// ComponentsList renders a table of components with their current state.
func ComponentsList(w io.Writer, m *model.Model, states *state.Derivation) {
	// Determine the longest component name for alignment.
	maxLen := 0
	for _, name := range m.Order {
		if len(name) > maxLen {
			maxLen = len(name)
		}
	}

	for _, name := range m.Order {
		st := "unknown"
		if states != nil {
			if s, ok := states.ComponentStates[name]; ok {
				st = s
			}
		}
		comp := m.Components[name]
		fmt.Fprintf(w, "  %-*s  type=%-12s state=%s\n", maxLen, name, comp.Type, st)
	}
}

// FactsList renders facts for a component (or all components if component is empty).
func FactsList(w io.Writer, store *facts.Store, component string) {
	components := store.AllComponents()
	sort.Strings(components)

	if component != "" {
		components = []string{component}
	}

	if len(components) == 0 {
		fmt.Fprintln(w, "  no facts recorded")
		return
	}

	for _, c := range components {
		ff := store.FactsFor(c)
		if len(ff) == 0 {
			continue
		}
		fmt.Fprintf(w, "  %s:\n", c)
		for _, f := range ff {
			fmt.Fprintf(w, "    %s = %v\n", f.Key, f.Value)
		}
	}
}

// Status renders a one-line health summary.
func Status(w io.Writer, store *facts.Store, states *state.Derivation) {
	if states == nil {
		fmt.Fprintln(w, "  no state derived")
		return
	}

	total := len(states.ComponentStates)
	healthy := 0
	unhealthy := 0
	unknown := 0

	for _, st := range states.ComponentStates {
		switch st {
		case "unknown":
			unknown++
		case "live":
			healthy++
		default:
			unhealthy++
		}
	}

	components := store.AllComponents()
	factCount := 0
	for _, c := range components {
		factCount += len(store.FactsFor(c))
	}

	fmt.Fprintf(w, "  %s: %d healthy, %d unhealthy, %d unknown | %s\n",
		Pluralize(total, "component", "components"),
		healthy, unhealthy, unknown,
		Pluralize(factCount, "fact", "facts"),
	)
}
