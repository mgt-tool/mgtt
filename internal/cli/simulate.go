package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/simulate"

	"github.com/spf13/cobra"
)

var simulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "Run failure scenarios against a model",
	RunE:  runSimulate,
}

var (
	simulateModel    string
	simulateScenario string
	simulateAll      bool
	scenariosDir     string
)

func init() {
	simulateCmd.Flags().StringVar(&simulateModel, "model", "system.model.yaml", "path to system.model.yaml")
	simulateCmd.Flags().StringVar(&simulateScenario, "scenario", "", "path to a single scenario YAML file")
	simulateCmd.Flags().BoolVar(&simulateAll, "all", false, "run all scenarios in the scenarios directory")
	simulateCmd.Flags().StringVar(&scenariosDir, "scenarios-dir", "scenarios", "directory containing scenario YAML files")
	simulateCmd.SilenceErrors = true
	rootCmd.AddCommand(simulateCmd)
}

func runSimulate(cmd *cobra.Command, args []string) error {
	if !simulateAll && simulateScenario == "" {
		return fmt.Errorf("specify --scenario <file> or --all")
	}

	// Load model.
	m, err := model.Load(simulateModel)
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}

	reg := providersupport.LoadAllEmbedded()

	w := cmd.OutOrStdout()

	if simulateAll {
		scenarios, err := simulate.LoadAllScenarios(scenariosDir)
		if err != nil {
			return err
		}

		var results []*simulate.Result
		for _, sc := range scenarios {
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
