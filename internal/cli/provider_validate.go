// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"

	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/validate"

	"github.com/spf13/cobra"
)

func newProviderValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <name>",
		Short: "Run static correctness checks on a provider",
		Long: `Validate a provider's manifest against the probe protocol.

Static checks (always safe):
  - meta.name and version populated
  - read_only: false requires writes_note describing the side effect
  - meta.requires.mgtt is satisfied by the running mgtt
  - runtime.entrypoint, when absolute, exists on disk
  - every fact has probe.cmd; warns if probe.parse is missing
  - default_active_state references a declared state
  - every runtime.needs capability resolves against the known vocabulary

Live checks against a real backend (--live) are scoped per-provider and
not yet implemented in core; provider repos run their own --live in CI.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := providersupport.LoadEmbedded(args[0])
			if err != nil {
				return fmt.Errorf("provider %q: %w", args[0], err)
			}
			rep := validate.Static(p)
			renderReport(cmd.OutOrStdout(), rep)
			if !rep.OK() {
				return fmt.Errorf("validation failed: %d failures", len(rep.Failures))
			}
			return nil
		},
	}
}

func renderReport(w io.Writer, r validate.Report) {
	for _, p := range r.Passed {
		fmt.Fprintln(w, "PASS  "+p)
	}
	for _, msg := range r.Warnings {
		fmt.Fprintln(w, "WARN  "+msg)
	}
	for _, msg := range r.Failures {
		fmt.Fprintln(w, "FAIL  "+msg)
	}
}
