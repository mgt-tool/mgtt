// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"time"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"

	"github.com/spf13/cobra"
)

func newFactAddCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "add <component> <key> <value>",
		Short: "Add a fact to the current incident",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			component, key, rawValue := args[0], args[1], args[2]
			inc, err := incident.Current()
			if err != nil {
				return fmt.Errorf("no active incident: %w", err)
			}
			value := expr.InferValue(rawValue)
			f := facts.Fact{
				Key:       key,
				Value:     value,
				Collector: "manual",
				At:        time.Now(),
				Note:      note,
			}
			if err := inc.Store.AppendAndSave(component, f); err != nil {
				return fmt.Errorf("saving fact: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  added %s.%s = %v\n", component, key, value)
			return nil
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "optional note for the fact")
	return cmd
}

func init() {
	registerCommand(func() *cobra.Command {
		factCmd := &cobra.Command{
			Use:   "fact",
			Short: "Manage facts",
		}
		factCmd.AddCommand(newFactAddCmd())
		return factCmd
	})
}
