package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
)

func init() {
	simulateCmd.Flags().StringVar(&simulateModel, "model", "system.model.yaml", "path to system.model.yaml")
	simulateCmd.Flags().StringVar(&simulateScenario, "scenario", "", "path to a single scenario YAML file")
	simulateCmd.Flags().BoolVar(&simulateAll, "all", false, "run all scenarios in the scenarios directory")
	simulateCmd.Flags().StringVar(&scenariosDir, "scenarios-dir", "scenarios", "directory containing scenario YAML files")
	simulateCmd.Flags().BoolVar(&simulateFromScenarios, "from-scenarios", false, "iterate enumerated scenarios as test cases; assert Occam identifies each root")
	simulateCmd.SilenceErrors = true
	rootCmd.AddCommand(simulateCmd)
}

func runSimulate(cmd *cobra.Command, args []string) error {
	if !simulateAll && simulateScenario == "" && !simulateFromScenarios {
		return fmt.Errorf("specify --scenario <file>, --all, or --from-scenarios")
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

	if simulateAll {
		cases, err := simulate.LoadAllScenarios(scenariosDir)
		if err != nil {
			return err
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
