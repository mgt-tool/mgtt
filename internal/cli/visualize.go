// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mgt-tool/mgtt/internal/model"

	"github.com/spf13/cobra"
)

type visualizeFlagSet struct {
	modelPath  string
	outputPath string
}

func newVisualizeCmd() *cobra.Command {
	f := &visualizeFlagSet{}
	cmd := &cobra.Command{
		Use:          "visualize",
		Short:        "Emit a mermaid-graph markdown file from the model",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVisualize(cmd, f)
		},
	}
	cmd.Flags().StringVar(&f.modelPath, "model", "",
		"path to model.yaml (default: auto-detect in cwd)")
	cmd.Flags().StringVar(&f.outputPath, "output", "",
		"output markdown path (default: <model-dir>/model-graph.md)")
	return cmd
}

func init() {
	registerCommand(newVisualizeCmd)
}

func runVisualize(cmd *cobra.Command, f *visualizeFlagSet) error {
	modelPath, err := resolveModelPath(f.modelPath)
	if err != nil {
		return err
	}
	m, err := model.Load(modelPath)
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}
	reg, err := loadRegistryForUse()
	if err != nil {
		return err
	}
	installed := buildInstalledList()
	body, err := model.Render(m, reg, installed)
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}
	out := f.outputPath
	if out == "" {
		out = filepath.Join(filepath.Dir(modelPath), "model-graph.md")
	}
	if err := os.WriteFile(out, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %d components to %s\n", len(m.Components), out)
	return nil
}
