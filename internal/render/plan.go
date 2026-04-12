package render

import (
	"fmt"
	"io"
	"strings"

	"mgtt/internal/engine"
)

// PlanSuggestion renders the current state of the path tree and the
// suggested next probe to w.
func PlanSuggestion(w io.Writer, tree *engine.PathTree) {
	// Show surviving paths.
	if len(tree.Paths) > 0 {
		fmt.Fprintf(w, "\n  %s to investigate:\n", Pluralize(len(tree.Paths), "path", "paths"))
		for _, p := range tree.Paths {
			fmt.Fprintf(w, "  %-8s %s\n", p.ID, strings.Join(p.Components, " <- "))
		}
	}

	// Show eliminated paths.
	if len(tree.Eliminated) > 0 {
		fmt.Fprintln(w)
		for _, p := range tree.Eliminated {
			fmt.Fprintf(w, "  %-8s %s  (eliminated: %s)\n", p.ID, strings.Join(p.Components, " <- "), p.Reason)
		}
	}

	// Show suggested probe.
	if tree.Suggested != nil {
		s := tree.Suggested
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  -> probe %s %s\n", s.Component, s.Fact)
		var meta []string
		if s.Cost != "" {
			meta = append(meta, "cost: "+s.Cost)
		}
		if s.Access != "" {
			meta = append(meta, s.Access)
		}
		if len(s.Eliminates) > 0 {
			meta = append(meta, "eliminates "+strings.Join(s.Eliminates, ", ")+" if healthy")
		}
		if len(meta) > 0 {
			fmt.Fprintf(w, "     %s\n", strings.Join(meta, " | "))
		}
	}
}

// ProbeResult renders the result of a single probe execution.
func ProbeResult(w io.Writer, component, fact string, value any, healthy bool) {
	mark := Checkmark(healthy)
	label := "healthy"
	if !healthy {
		label = "unhealthy"
	}
	fmt.Fprintf(w, "\n  %s %s.%s = %v   %s %s\n", Checkmark(true), component, fact, value, mark, label)
}

// RootCauseSummary renders the final root cause determination.
func RootCauseSummary(w io.Writer, tree *engine.PathTree) {
	if tree.RootCause == "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  All components healthy -- no root cause found.")
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Root cause: %s\n", tree.RootCause)

	// Show the root cause path.
	for _, p := range tree.Paths {
		last := p.Components[len(p.Components)-1]
		if last == tree.RootCause {
			fmt.Fprintf(w, "  Path:       %s\n", strings.Join(p.Components, " <- "))
			break
		}
	}

	// Show state.
	if tree.States != nil {
		if st, ok := tree.States.ComponentStates[tree.RootCause]; ok {
			fmt.Fprintf(w, "  State:      %s\n", st)
		}
	}

	// Show eliminated components (only those NOT on surviving paths).
	if len(tree.Eliminated) > 0 {
		surviving := map[string]bool{}
		for _, p := range tree.Paths {
			for _, c := range p.Components {
				surviving[c] = true
			}
		}
		var names []string
		seen := map[string]bool{}
		for _, p := range tree.Eliminated {
			last := p.Components[len(p.Components)-1]
			if !seen[last] && !surviving[last] {
				seen[last] = true
				names = append(names, last)
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(w, "  Eliminated: %s\n", strings.Join(names, ", "))
		}
	}
}

// PlanHeader renders the initial entry point message.
func PlanHeader(w io.Writer, entry string) {
	fmt.Fprintf(w, "\n  starting from outermost component: %s\n", entry)
}
