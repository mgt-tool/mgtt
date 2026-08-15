// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"

	"github.com/mgt-tool/mgtt/internal/engine"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/state"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var modelPath string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show one-line health summary",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := model.Load(modelPath)
			if err != nil {
				return fmt.Errorf("load model: %w", err)
			}
			reg, err := loadRegistryForUse()
			if err != nil {
				return err
			}
			store := incident.CurrentStoreOrMemory()
			derivation := state.Derive(m, reg, store)
			renderStatus(cmd.OutOrStdout(), m, reg, store, derivation)
			return nil
		},
	}
	cmd.Flags().StringVar(&modelPath, "model", "system.model.yaml", "path to system.model.yaml")
	return cmd
}

func init() {
	registerCommand(newStatusCmd)
}

// renderStatus renders a one-line health summary.
func renderStatus(w io.Writer, m *model.Model, reg *providersupport.Registry, store *facts.Store, states *state.Derivation) {
	if states == nil {
		fmt.Fprintln(w, "  no state derived")
		return
	}

	total := len(states.ComponentStates)
	healthy := 0
	unhealthy := 0
	unknown := 0

	for name, st := range states.ComponentStates {
		switch {
		case st == "unknown":
			unknown++
		case st == engine.ResolveDefaultActive(m.Components[name], m, reg):
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
