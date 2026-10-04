// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/scenarios"

	"github.com/spf13/cobra"
)

func newModelImpactCmd() *cobra.Command {
	var (
		modelPath string
		states    []string
	)
	cmd := &cobra.Command{
		Use:   "impact <component>",
		Short: "Show what breaks if a component fails, from the model alone",
		Long: "Walk the model's failure graph from a component's failure states and\n" +
			"list every component the failure reaches, with a shortest chain to it.\n" +
			"Symptoms are the components nothing depends on, where users see it.\n" +
			"Redundancy groups that the failure does not overwhelm stop it; chains\n" +
			"that depend on a while: guard say so. No facts, no live system.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveModelPath(modelPath)
			if err != nil {
				return err
			}
			m, err := model.Load(path)
			if err != nil {
				return fmt.Errorf("load model: %w", err)
			}
			reg, err := loadRegistryForUse()
			if err != nil {
				return err
			}
			imp, err := scenarios.ImpactOf(scenarios.BuildGraph(m, reg), m, args[0], states)
			if err != nil {
				return err
			}
			renderImpact(cmd.OutOrStdout(), imp)
			return nil
		},
	}
	cmd.Flags().StringVar(&modelPath, "model", "", "path to system.model.yaml (default: auto-detect)")
	cmd.Flags().StringSliceVar(&states, "state", nil, "only these failure states (default: all)")
	return cmd
}

func renderImpact(w io.Writer, imp *scenarios.Impact) {
	fmt.Fprintf(w, "If %s fails (%s):\n\n", imp.Component, strings.Join(imp.States, ", "))
	if len(imp.Affected) == 0 {
		fmt.Fprintln(w, "  nothing else breaks.")
	}
	for _, a := range imp.Affected {
		mark := " "
		if a.Symptom {
			mark = "!"
		}
		fmt.Fprintf(w, "  %s %-28s %s\n", mark, a.Component, strings.Join(a.Path, " → "))
		if len(a.Conditions) > 0 {
			fmt.Fprintf(w, "      only while one of: %s\n", strings.Join(a.Conditions, "; "))
		}
	}
	for _, b := range imp.Blocked {
		fmt.Fprintf(w, "  = %s holds: redundancy covers %s\n", b.Dependent, b.Member)
	}
	n := 0
	for _, a := range imp.Affected {
		if a.Symptom {
			n++
		}
	}
	fmt.Fprintf(w, "\n%d component(s) affected, %d user-facing (!)\n", len(imp.Affected), n)
}
