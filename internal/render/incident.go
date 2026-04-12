package render

import (
	"fmt"
	"io"
	"sort"

	"mgtt/internal/facts"
	"mgtt/internal/incident"
)

// IncidentStart renders a confirmation that an incident has been started.
func IncidentStart(w io.Writer, inc *incident.Incident) {
	fmt.Fprintf(w, "  %s %s started\n", Checkmark(true), inc.ID)
	fmt.Fprintf(w, "    model: %s v%s\n", inc.Model, inc.Version)
	fmt.Fprintf(w, "    state: %s\n", inc.StateFile)
}

// IncidentEnd renders an incident closure summary.
func IncidentEnd(w io.Writer, inc *incident.Incident, store *facts.Store) {
	duration := inc.Ended.Sub(inc.Started)
	fmt.Fprintf(w, "  %s %s ended\n", Checkmark(true), inc.ID)
	fmt.Fprintf(w, "    duration: %s\n", duration.Round(1e9)) // round to seconds

	// Count facts.
	components := store.AllComponents()
	sort.Strings(components)
	total := 0
	for _, c := range components {
		total += len(store.FactsFor(c))
	}
	fmt.Fprintf(w, "    facts:    %s across %s\n",
		Pluralize(total, "fact", "facts"),
		Pluralize(len(components), "component", "components"),
	)
}
