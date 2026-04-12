package cli

import (
	"fmt"

	"mgtt/internal/facts"
	"mgtt/internal/incident"
	"mgtt/internal/model"
	"mgtt/internal/provider"
	"mgtt/internal/render"
	"mgtt/internal/state"

	"github.com/spf13/cobra"
)

var statusModelPath string

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show one-line health summary",
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := model.Load(statusModelPath)
		if err != nil {
			return fmt.Errorf("load model: %w", err)
		}

		reg := provider.NewRegistry()
		for _, name := range provider.ListEmbedded() {
			p, err := provider.LoadEmbedded(name)
			if err == nil {
				reg.Register(p)
			}
		}

		var store *facts.Store
		if inc, err := incident.Current(); err == nil {
			store = inc.Store
		} else {
			store = facts.NewInMemory()
		}

		derivation := state.Derive(m, reg, store)
		render.Status(cmd.OutOrStdout(), store, derivation)
		return nil
	},
}

func init() {
	statusCmd.Flags().StringVar(&statusModelPath, "model", "system.model.yaml", "path to system.model.yaml")
	rootCmd.AddCommand(statusCmd)
}
