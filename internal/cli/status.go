package cli

import (
	"fmt"
	"io"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/state"

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

		reg, err := loadRegistryForUse()
		if err != nil {
			return err
		}

		var store *facts.Store
		if inc, err := incident.Current(); err == nil {
			store = inc.Store
		} else {
			store = facts.NewInMemory()
		}

		derivation := state.Derive(m, reg, store)
		renderStatus(cmd.OutOrStdout(), store, derivation)
		return nil
	},
}

func init() {
	statusCmd.Flags().StringVar(&statusModelPath, "model", "system.model.yaml", "path to system.model.yaml")
	rootCmd.AddCommand(statusCmd)
}

// renderStatus renders a one-line health summary.
func renderStatus(w io.Writer, store *facts.Store, states *state.Derivation) {
	if states == nil {
		fmt.Fprintln(w, "  no state derived")
		return
	}

	total := len(states.ComponentStates)
	healthy := 0
	unhealthy := 0
	unknown := 0

	for _, st := range states.ComponentStates {
		switch st {
		case "unknown":
			unknown++
		case "live":
			healthy++
		default:
			unhealthy++
		}
	}

	components := store.AllComponents()
	factCount := 0
	for _, c := range components {
		factCount += len(store.FactsFor(c))
	}

	fmt.Fprintf(w, "  %s: %d healthy, %d unhealthy, %d unknown | %s\n",
		pluralize(total, "component", "components"),
		healthy, unhealthy, unknown,
		pluralize(factCount, "fact", "facts"),
	)
}
