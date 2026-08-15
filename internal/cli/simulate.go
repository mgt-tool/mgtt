// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
	"github.com/mgt-tool/mgtt/internal/simulate"

	"github.com/spf13/cobra"
)

type simulateFlags struct {
	model         string
	scenario      string
	all           bool
	scenariosDir  string
	fromScenarios bool
	fuzzN         int
	fuzzSeed      int64
}

func newSimulateCmd() *cobra.Command {
	f := &simulateFlags{}
	cmd := &cobra.Command{
		Use:   "simulate",
		Short: "Run failure scenarios against a model",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSimulate(cmd, f, args)
		},
	}
	cmd.Flags().StringVar(&f.model, "model", "system.model.yaml", "path to system.model.yaml")
	cmd.Flags().StringVar(&f.scenario, "scenario", "", "path to a single scenario YAML file")
	cmd.Flags().BoolVar(&f.all, "all", false, "run all scenarios in the scenarios directory")
	cmd.Flags().StringVar(&f.scenariosDir, "scenarios-dir", "scenarios", "directory containing scenario YAML files")
	cmd.Flags().BoolVar(&f.fromScenarios, "from-scenarios", false, "iterate enumerated scenarios as test cases; assert Occam identifies each root")
	cmd.Flags().IntVar(&f.fuzzN, "fuzz", 0, "run N fuzz iterations: random scenario, random fact-trail truncation, assert convergence")
	cmd.Flags().Int64Var(&f.fuzzSeed, "fuzz-seed", 0, "seed for --fuzz (default: time-based)")
	cmd.SilenceErrors = true
	return cmd
}

func init() {
	registerCommand(newSimulateCmd)
}

func runSimulate(cmd *cobra.Command, f *simulateFlags, args []string) error {
	if !f.all && f.scenario == "" && !f.fromScenarios && f.fuzzN == 0 {
		return fmt.Errorf("specify --scenario <file>, --all, --from-scenarios, or --fuzz N")
	}

	m, err := model.Load(f.model)
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}
	warnLegacyProviderRefs(os.Stderr, m)
	reg, err := loadRegistryForUse()
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()

	switch {
	case f.fromScenarios:
		return runFromScenariosMode(w, m, reg, f.model)
	case f.fuzzN > 0:
		return runFuzzMode(w, m, reg, f)
	case f.all:
		return runAllScenariosMode(w, m, reg, f)
	default:
		return runSingleScenarioMode(w, m, reg, f)
	}
}

func runFromScenariosMode(w io.Writer, m *model.Model, reg *providersupport.Registry, modelPath string) error {
	scs, err := loadEnumeratedScenariosForModel(modelPath)
	if err != nil {
		return err
	}
	p, fc, details := simulate.RunFromScenarios(m, reg, scs)
	for _, d := range details {
		fmt.Fprintln(w, d)
	}
	fmt.Fprintf(w, "%d/%d scenarios passed\n", p, p+fc)
	if fc > 0 {
		return fmt.Errorf("%d scenario(s) failed", fc)
	}
	return nil
}

func runFuzzMode(w io.Writer, m *model.Model, reg *providersupport.Registry, f *simulateFlags) error {
	scs, err := loadEnumeratedScenariosForModel(f.model)
	if err != nil {
		return err
	}
	seed := f.fuzzSeed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	p, fc, details := simulate.Fuzz(m, reg, scs, f.fuzzN, seed)
	for _, d := range details {
		fmt.Fprintln(w, d)
	}
	fmt.Fprintf(w, "fuzz seed=%d  %d/%d iterations passed\n", seed, p, p+fc)
	if fc > 0 {
		return fmt.Errorf("%d fuzz iteration(s) failed", fc)
	}
	return nil
}

func runAllScenariosMode(w io.Writer, m *model.Model, reg *providersupport.Registry, f *simulateFlags) error {
	cases, err := simulate.LoadAllScenarios(f.scenariosDir)
	if err != nil {
		return err
	}
	enumerated, _ := loadEnumeratedScenariosForModel(f.model)
	for _, c := range cases {
		emitGapWarning(w, c, enumerated)
	}
	results := make([]*simulate.Result, 0, len(cases))
	for _, sc := range cases {
		results = append(results, simulate.Run(m, reg, sc))
	}
	renderSimulateAll(w, results)
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

func runSingleScenarioMode(w io.Writer, m *model.Model, reg *providersupport.Registry, f *simulateFlags) error {
	sc, err := simulate.LoadScenario(f.scenario)
	if err != nil {
		return err
	}
	enumerated, _ := loadEnumeratedScenariosForModel(f.model)
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
	scs, _, err := scenarios.LoadSiblingOf(modelPath)
	return scs, err
}
