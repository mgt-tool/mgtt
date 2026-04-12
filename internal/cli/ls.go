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

var lsModelPath string

var lsCmd = &cobra.Command{
	Use:   "ls [components|facts]",
	Short: "List components or facts",
	RunE: func(cmd *cobra.Command, args []string) error {
		subcommand := "components"
		if len(args) > 0 {
			subcommand = args[0]
		}
		switch subcommand {
		case "components":
			return lsComponents(cmd)
		case "facts":
			component := ""
			if len(args) > 1 {
				component = args[1]
			}
			return lsFacts(cmd, component)
		default:
			return fmt.Errorf("unknown subcommand %q (expected 'components' or 'facts')", subcommand)
		}
	},
}

func init() {
	lsCmd.Flags().StringVar(&lsModelPath, "model", "system.model.yaml", "path to system.model.yaml")
	rootCmd.AddCommand(lsCmd)
}

func lsComponents(cmd *cobra.Command) error {
	m, err := model.Load(lsModelPath)
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

	// Try to load facts from current incident for state derivation.
	var derivation *state.Derivation
	if inc, err := incident.Current(); err == nil {
		derivation = state.Derive(m, reg, inc.Store)
	} else {
		// No incident — derive with empty store.
		derivation = state.Derive(m, reg, facts.NewInMemory())
	}

	render.ComponentsList(cmd.OutOrStdout(), m, derivation)
	return nil
}

func lsFacts(cmd *cobra.Command, component string) error {
	inc, err := incident.Current()
	if err != nil {
		return fmt.Errorf("no active incident: %w", err)
	}

	render.FactsList(cmd.OutOrStdout(), inc.Store, component)
	return nil
}
