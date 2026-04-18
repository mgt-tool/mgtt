package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/state"

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

	reg, err := loadRegistryAll()
	if err != nil {
		return err
	}

	store := facts.NewInMemory()
	if inc, err := incident.Current(); err == nil {
		store = inc.Store
	}
	derivation := state.Derive(m, reg, store)

	renderComponentsList(cmd.OutOrStdout(), m, derivation)
	return nil
}

func lsFacts(cmd *cobra.Command, component string) error {
	inc, err := incident.Current()
	if err != nil {
		return fmt.Errorf("no active incident: %w", err)
	}

	renderFactsList(cmd.OutOrStdout(), inc.Store, component)
	return nil
}

// renderComponentsList renders a table of components with their current state.
func renderComponentsList(w io.Writer, m *model.Model, states *state.Derivation) {
	// Determine the longest component name for alignment.
	maxLen := 0
	for _, name := range m.Order {
		if len(name) > maxLen {
			maxLen = len(name)
		}
	}

	for _, name := range m.Order {
		st := "unknown"
		if states != nil {
			if s, ok := states.ComponentStates[name]; ok {
				st = s
			}
		}
		comp := m.Components[name]
		fmt.Fprintf(w, "  %-*s  type=%-12s state=%s\n", maxLen, name, comp.Type, st)
	}
}

// renderFactsList renders facts for a component (or all components if component is empty).
func renderFactsList(w io.Writer, store *facts.Store, component string) {
	components := store.AllComponents()
	sort.Strings(components)

	if component != "" {
		components = []string{component}
	}

	if len(components) == 0 {
		fmt.Fprintln(w, "  no facts recorded")
		return
	}

	for _, c := range components {
		ff := store.FactsFor(c)
		if len(ff) == 0 {
			continue
		}
		fmt.Fprintf(w, "  %s:\n", c)
		for _, f := range ff {
			fmt.Fprintf(w, "    %s = %v\n", f.Key, f.Value)
		}
	}
}
