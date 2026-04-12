package cli

import (
	"fmt"
	"os"

	"mgtt/internal/model"
	"mgtt/internal/provider"
	"mgtt/internal/render"
	"mgtt/internal/simulate"

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

	// Load providers.
	reg := provider.NewRegistry()
	for _, name := range provider.ListEmbedded() {
		p, err := provider.LoadEmbedded(name)
		if err == nil {
			reg.Register(p)
		}
	}

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

		render.SimulateAll(w, results)

		// Exit with error if any scenario failed.
		for _, r := range results {
			if !r.Pass {
				os.Exit(1)
			}
		}
		return nil
	}

	// Single scenario.
	sc, err := simulate.LoadScenario(simulateScenario)
	if err != nil {
		return err
	}

	result := simulate.Run(m, reg, sc)
	render.SimulateResult(w, result)

	if !result.Pass {
		os.Exit(1)
	}
	return nil
}
