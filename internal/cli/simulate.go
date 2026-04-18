package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/scenarios"
	"github.com/mgt-tool/mgtt/internal/simulate"

	"github.com/spf13/cobra"
)

var simulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "Run failure scenarios against a model",
	RunE:  runSimulate,
}

var (
	simulateModel         string
	simulateScenario      string
	simulateAll           bool
	scenariosDir          string
	simulateFromScenarios bool
	simulateFuzzN         int
	simulateFuzzSeed      int64
)

func init() {
	simulateCmd.Flags().StringVar(&simulateModel, "model", "system.model.yaml", "path to system.model.yaml")
	simulateCmd.Flags().StringVar(&simulateScenario, "scenario", "", "path to a single scenario YAML file")
	simulateCmd.Flags().BoolVar(&simulateAll, "all", false, "run all scenarios in the scenarios directory")
	simulateCmd.Flags().StringVar(&scenariosDir, "scenarios-dir", "scenarios", "directory containing scenario YAML files")
	simulateCmd.Flags().BoolVar(&simulateFromScenarios, "from-scenarios", false, "iterate enumerated scenarios as test cases; assert Occam identifies each root")
	simulateCmd.Flags().IntVar(&simulateFuzzN, "fuzz", 0, "run N fuzz iterations: random scenario, random fact-trail truncation, assert convergence")
	simulateCmd.Flags().Int64Var(&simulateFuzzSeed, "fuzz-seed", 0, "seed for --fuzz (default: time-based)")
	simulateCmd.SilenceErrors = true
	rootCmd.AddCommand(simulateCmd)
}

func runSimulate(cmd *cobra.Command, args []string) error {
	if !simulateAll && simulateScenario == "" && !simulateFromScenarios && simulateFuzzN == 0 {
		return fmt.Errorf("specify --scenario <file>, --all, --from-scenarios, or --fuzz N")
	}

	// Load model.
	m, err := model.Load(simulateModel)
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}

	// Emit FQN deprecation warnings even in simulation.
	for _, name := range m.Meta.Providers {
		ref, refErr := model.ParseProviderRef(name)
		if refErr != nil {
			continue // bad ref; validate catches this
		}
		if ref.LegacyBareName {
			fmt.Fprintf(os.Stderr, "⚠ model uses bare provider name %q; consider %q\n",
				ref.Name, "<namespace>/"+ref.Name+"@<version>")
		}
	}

	reg, err := loadRegistryForUse()
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()

	// Enumerated-scenarios iteration (Task F1).
	if simulateFromScenarios {
		scs, err := loadEnumeratedScenariosForModel(simulateModel)
		if err != nil {
			return err
		}
		p, f, details := simulate.RunFromScenarios(m, reg, scs)
		for _, d := range details {
			fmt.Fprintln(w, d)
		}
		fmt.Fprintf(w, "%d/%d scenarios passed\n", p, p+f)
		if f > 0 {
			return fmt.Errorf("%d scenario(s) failed", f)
		}
		return nil
	}

	// Fuzz mode (Task F2).
	if simulateFuzzN > 0 {
		scs, err := loadEnumeratedScenariosForModel(simulateModel)
		if err != nil {
			return err
		}
		seed := simulateFuzzSeed
		if seed == 0 {
			seed = time.Now().UnixNano()
		}
		p, f, details := simulate.Fuzz(m, reg, scs, simulateFuzzN, seed)
		for _, d := range details {
			fmt.Fprintln(w, d)
		}
		fmt.Fprintf(w, "fuzz seed=%d  %d/%d iterations passed\n", seed, p, p+f)
		if f > 0 {
			return fmt.Errorf("%d fuzz iteration(s) failed", f)
		}
		return nil
	}

	// Hand-authored path — collect enumerated scenarios (best-effort) for
	// gap-detection warnings.
	enumerated, _ := loadEnumeratedScenariosForModel(simulateModel)

	if simulateAll {
		cases, err := simulate.LoadAllScenarios(scenariosDir)
		if err != nil {
			return err
		}

		for _, c := range cases {
			emitGapWarning(w, c, enumerated)
		}

		var results []*simulate.Result
		for _, sc := range cases {
			results = append(results, simulate.Run(m, reg, sc))
		}

		renderSimulateAll(w, results)

		// Return error (without cobra printing it) if any scenario failed.
		failCount := 0
		for _, r := range results {
			if !r.Pass {
				failCount++
			}
		}
		if failCount > 0 {
			return fmt.Errorf("%d scenario(s) failed", failCount)
		}
		return nil
	}

	// Single scenario.
	sc, err := simulate.LoadScenario(simulateScenario)
	if err != nil {
		return err
	}

	emitGapWarning(w, sc, enumerated)

	result := simulate.Run(m, reg, sc)
	renderSimulateResult(w, result)

	if !result.Pass {
		return fmt.Errorf("1 scenario(s) failed")
	}
	return nil
}

// renderSimulateResult writes the result of a single simulation scenario.
func renderSimulateResult(w io.Writer, result *simulate.Result) {
	if result.Pass {
		fmt.Fprintf(w, "  %-40s %s passed\n", result.Scenario.Name, checkmark(true))
	} else {
		fmt.Fprintf(w, "  %-40s %s FAILED\n", result.Scenario.Name, checkmark(false))
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

// renderSimulateAll writes a summary of all simulation results.
func renderSimulateAll(w io.Writer, results []*simulate.Result) {
	passed := 0
	for _, r := range results {
		renderSimulateResult(w, r)
		if r.Pass {
			passed++
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %d/%d scenarios passed\n", passed, len(results))
}

// emitGapWarning prints a warning when a hand-authored simulate case
// expects a root cause that no enumerated scenario chains to with a
// terminal symptom on one of the case's injected components. Silenced
// when the case sets `unenumerated_intentional: true` or when no
// enumerated scenarios are available.
//
// A scenario "matches" the case only when:
//   - its root component equals the case's expect.root_cause, AND
//   - its terminal step (the one with Observes) is on a component the
//     case has injected facts for
//
// Rationale: an enumerated scenario with the right root but chaining
// through a different symptom path (terminal on a component the case
// never touches) isn't exercising the path this case is set up for;
// treating it as a match would silently accept gaps in coverage.
func emitGapWarning(w io.Writer, c *simulate.Scenario, enumerated []scenarios.Scenario) {
	if c == nil || c.UnenumeratedIntentional || len(enumerated) == 0 {
		return
	}
	if c.Expect.RootCause == "" || c.Expect.RootCause == "none" {
		return
	}
	injected := map[string]bool{}
	for comp := range c.Inject {
		injected[comp] = true
	}
	for _, e := range enumerated {
		if e.Root.Component != c.Expect.RootCause {
			continue
		}
		terminal := scenarioTerminalComponent(e)
		// If the case has no injected components at all, fall back to
		// the root-only check — otherwise we'd warn on every case even
		// when an obvious match exists.
		if len(injected) == 0 {
			return
		}
		if terminal != "" && injected[terminal] {
			return
		}
	}
	fmt.Fprintf(w, "WARN: simulate case %q expects root=%s, but no enumerated scenario chains that root to an observed symptom on an injected component. Add triggered_by labels, or mark the case with `unenumerated_intentional: true`.\n",
		c.Name, c.Expect.RootCause)
}

// scenarioTerminalComponent returns the component of the step marked
// terminal (has Observes). Returns "" when the scenario has no
// terminal step.
func scenarioTerminalComponent(s scenarios.Scenario) string {
	for _, step := range s.Chain {
		if step.IsTerminal() {
			return step.Component
		}
	}
	return ""
}

// loadEnumeratedScenariosForModel reads the sibling scenarios.yaml for
// the given model path. Returns an empty list (no error) when the file
// is missing — the caller decides whether that's fatal.
func loadEnumeratedScenariosForModel(modelPath string) ([]scenarios.Scenario, error) {
	scPath := filepath.Join(filepath.Dir(modelPath), "scenarios.yaml")
	f, err := os.Open(scPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", scPath, err)
	}
	defer f.Close()
	scs, _, err := scenarios.Read(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", scPath, err)
	}
	return scs, nil
}
