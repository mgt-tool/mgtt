// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mgt-tool/mgtt/internal/model"

	"github.com/spf13/cobra"
)

// newModelExportCmd builds `mgtt model export`.
//
// The document it emits is the resolved model — provider types merged in,
// component overrides applied — which is what any consumer outside mgtt
// needs and the least it needs: no registry, no install directory, no
// credentials. `writ mgtt` is the first such consumer.
//
// --json is required rather than defaulted even though it is the only
// format, so that adding a second one later cannot silently change what an
// existing invocation produces.
func newModelExportCmd() *cobra.Command {
	var (
		outputPath string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "export [path]",
		Short: "Emit the resolved model (types merged, overrides applied) as JSON",
		Long: "Emit the resolved model as a versioned JSON document.\n\n" +
			"Provider types are merged into the components that use them and\n" +
			"component-level overrides are already applied, so a consumer needs\n" +
			"no provider registry of its own. This is the document `writ mgtt`\n" +
			"reads.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit := ""
			if len(args) > 0 {
				explicit = args[0]
			}
			return runModelExport(cmd, explicit, outputPath, asJSON)
		},
	}
	cmd.Flags().StringVar(&outputPath, "output", "",
		"write here instead of stdout")
	cmd.Flags().BoolVar(&asJSON, "json", false,
		"emit JSON (required; currently the only format)")
	return cmd
}

func runModelExport(cmd *cobra.Command, explicitPath, outputPath string, asJSON bool) error {
	if !asJSON {
		return fmt.Errorf("--json is required (it is currently the only format)")
	}

	modelPath, err := resolveModelPath(explicitPath)
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
	body, err := model.ExportJSON(m, reg)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	reportExportDeclines(cmd, body)

	if outputPath == "" {
		_, err := cmd.OutOrStdout().Write(body)
		return err
	}
	if err := os.WriteFile(outputPath, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	// The note goes to stderr so that a run with --output stays silent on
	// stdout, which is what a caller redirecting stdout expects.
	fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s\n", outputPath)
	return nil
}

// reportExportDeclines echoes the document's own declines to stderr.
//
// They already travel inside the JSON, for the consumer. This is for the
// person: the common way to run this command is with stdout redirected into a
// file, and a decline nobody sees is a decline that did not happen.
func reportExportDeclines(cmd *cobra.Command, body []byte) {
	var doc struct {
		Declines []struct {
			What string `json:"what"`
			Why  string `json:"why"`
		} `json:"declines"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || len(doc.Declines) == 0 {
		return
	}
	w := cmd.ErrOrStderr()
	fmt.Fprintln(w, "declined:")
	for _, d := range doc.Declines {
		fmt.Fprintf(w, "  %s: %s\n", d.What, d.Why)
	}
}
