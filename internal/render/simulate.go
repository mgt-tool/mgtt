package render

import (
	"fmt"
	"io"
	"strings"

	"mgtt/internal/simulate"
)

// SimulateResult writes the result of a single simulation scenario.
func SimulateResult(w io.Writer, result *simulate.Result) {
	if result.Pass {
		fmt.Fprintf(w, "  %-40s %s passed\n", result.Scenario.Name, Checkmark(true))
	} else {
		fmt.Fprintf(w, "  %-40s %s FAILED\n", result.Scenario.Name, Checkmark(false))
		fmt.Fprintf(w, "    expected: root_cause=%s path=[%s] eliminated=[%s]\n",
			result.Scenario.Expect.RootCause,
			strings.Join(result.Scenario.Expect.Path, ", "),
			strings.Join(result.Scenario.Expect.Eliminated, ", "),
		)
		fmt.Fprintf(w, "    actual:   root_cause=%s path=[%s] eliminated=[%s]\n",
			result.Actual.RootCause,
			strings.Join(result.Actual.Path, ", "),
			strings.Join(result.Actual.Eliminated, ", "),
		)
	}
}

// SimulateAll writes a summary of all simulation results.
func SimulateAll(w io.Writer, results []*simulate.Result) {
	passed := 0
	for _, r := range results {
		SimulateResult(w, r)
		if r.Pass {
			passed++
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %d/%d scenarios passed\n", passed, len(results))
}
