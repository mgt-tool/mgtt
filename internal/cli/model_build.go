// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/model/build"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/sdk/provider"

	"github.com/spf13/cobra"
)

// modelBuildFlags is the parsed set of command flags. Tests construct
// it directly; the cobra command populates it at runtime.
type modelBuildFlags struct {
	mgttHome     string
	output       string
	allowDeletes bool
	tombstone    []string
	dryRun       bool
}

// runModelBuild is the testable core: no cobra, no globals. Reads
// the existing model (if present), invokes every installed provider's
// discover, builds + diffs + gates + writes. Returns an exit code.
func runModelBuild(ctx context.Context, f modelBuildFlags, stdout, stderr io.Writer) int {
	prev, code := loadPrevModel(f.output, stderr)
	if code != 0 {
		return code
	}
	plan, err := build.Discover(ctx, f.mgttHome, prev, f.tombstone)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	reportDiscoverFailures(stderr, plan.Failures)
	if f.dryRun {
		renderBuildSummary(stdout, plan.Snapshots, plan.Next, plan.Diff)
		renderAuthored(stdout, plan.Next, plan.Kept)
		if plan.Diff.HasDeletions() {
			fmt.Fprintf(stdout, "  A real build would refuse these removals without --allow-deletes or --tombstone.\n")
		}
		fmt.Fprintln(stdout, "  Dry run: nothing written.")
		return 0
	}
	if code := gateDeletions(stderr, plan.Diff, f); code != 0 {
		return code
	}
	renderBuildSummary(stdout, plan.Snapshots, plan.Next, plan.Diff)
	renderAuthored(stdout, plan.Next, plan.Kept)
	return writeBuiltModel(stdout, stderr, f.output, plan.Next)
}

// reportDiscoverFailures prints one line per provider that contributed
// nothing, in deterministic (sorted) order: "no Discover() support" when
// the provider has no discovery, "discover failed" when it ran and failed.
func reportDiscoverFailures(w io.Writer, failures map[string]error) {
	keys := make([]string, 0, len(failures))
	for name := range failures {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		if errors.Is(failures[name], providersupport.ErrNoDiscover) {
			fmt.Fprintf(w, "  %s provider → no Discover() support (skipped)\n", name)
			continue
		}
		fmt.Fprintf(w, "  %s provider → discover failed (skipped): %v\n", name, failures[name])
	}
}

// loadPrevModel returns the committed model at path, or (nil, 0) when
// the file doesn't exist. Returns a non-zero exit code on read failure.
func loadPrevModel(path string, stderr io.Writer) (*model.Model, int) {
	if _, err := os.Stat(path); err != nil {
		return nil, 0
	}
	prev, err := model.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "load existing %s: %v\n", path, err)
		return nil, 1
	}
	return prev, 0
}

// renderAuthored names the authored components kept although discovery
// does not return them, and any of their dependencies on a component the
// model no longer has.
func renderAuthored(w io.Writer, next *model.Model, kept []string) {
	if len(kept) > 0 {
		fmt.Fprintf(w, "  Kept (authored, not from discovery): %s\n", strings.Join(kept, ", "))
		fmt.Fprintln(w, "    a component without `source: discovered` is never removed by build; delete it by hand if it is gone")
	}
	for _, d := range build.Dangling(next) {
		fmt.Fprintf(w, "  Dangling: %s, which the model no longer has\n", d)
	}
}

// gateDeletions enforces the deletion safety contract. Returns a non-zero
// exit code when the gate refused the build and prints a friendly summary.
func gateDeletions(stderr io.Writer, diff build.Diff, f modelBuildFlags) int {
	err := build.GateDeletions(diff, build.GateFlags{
		AllowDeletes: f.allowDeletes,
		Tombstone:    f.tombstone,
	})
	if err == nil {
		return 0
	}
	if errors.Is(err, build.ErrDeletionsRefused) {
		fmt.Fprintln(stderr, "Model drift detected (vs committed "+f.output+"):")
		for _, rm := range diff.Removed {
			fmt.Fprintf(stderr, "  -  %s\n", rm)
		}
		fmt.Fprintln(stderr)
		// Strip the sentinel prefix GateDeletions' Errorf prepended so
		// only the options-hint body survives.
		body := strings.TrimPrefix(err.Error(), build.ErrDeletionsRefused.Error()+": ")
		fmt.Fprintln(stderr, body)
		return 1
	}
	fmt.Fprintf(stderr, "gate: %v\n", err)
	return 1
}

// renderBuildSummary prints per-provider counts and the aggregate
// added/removed component lists in deterministic order.
func renderBuildSummary(w io.Writer, snapshots map[string]provider.DiscoveryResult, next *model.Model, diff build.Diff) {
	keys := make([]string, 0, len(snapshots))
	for k := range snapshots {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		snap := snapshots[name]
		fmt.Fprintf(w, "  %s provider    → %d components, %d dependencies\n", name, len(snap.Components), len(snap.Dependencies))
	}
	fmt.Fprintf(w, "\n  Model: %d components\n", len(next.Components))
	if len(diff.Added) > 0 {
		fmt.Fprintf(w, "  Added:   %s\n", strings.Join(diff.Added, ", "))
	}
	if len(diff.Removed) > 0 {
		fmt.Fprintf(w, "  Removed: %s\n", strings.Join(diff.Removed, ", "))
	}
}

// writeBuiltModel serialises m to path with explicit close-error handling.
// Returns a non-zero exit code for any IO failure along the way.
func writeBuiltModel(stdout, stderr io.Writer, path string, m *model.Model) int {
	outFile, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(stderr, "open output: %v\n", err)
		return 1
	}
	if err := build.EmitYAML(m, outFile); err != nil {
		_ = outFile.Close()
		fmt.Fprintf(stderr, "emit: %v\n", err)
		return 1
	}
	if err := outFile.Close(); err != nil {
		fmt.Fprintf(stderr, "close %s: %v\n", path, err)
		return 1
	}
	fmt.Fprintf(stdout, "  Written: %s\n", path)
	return 0
}

// newModelBuildCmd wires runModelBuild into cobra.
func newModelBuildCmd() *cobra.Command {
	f := modelBuildFlags{}
	cmd := &cobra.Command{
		Use:          "build",
		Short:        "Generate system.model.yaml from installed providers' Discover() output",
		SilenceUsage: true,
		Long: "Invokes every installed provider's discover subcommand,\n" +
			"merges results, gates deletions, writes a deterministic YAML\n" +
			"model. Commit the result to version control.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.mgttHome == "" {
				home, err := providersupport.Home()
				if err != nil {
					return fmt.Errorf("resolve MGTT_HOME: %w", err)
				}
				f.mgttHome = home
			}
			if f.output == "" {
				f.output = "system.model.yaml"
			}
			code := runModelBuild(cmd.Context(), f, cmd.OutOrStdout(), cmd.ErrOrStderr())
			if code != 0 {
				return fmt.Errorf("exit %d", code)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&f.mgttHome, "mgtt-home", "", "override $MGTT_HOME for discovery (default: $MGTT_HOME or ~/.mgtt)")
	cmd.Flags().StringVar(&f.output, "output", "", "output path (default: system.model.yaml)")
	cmd.Flags().BoolVar(&f.allowDeletes, "allow-deletes", false, "accept removal of components no longer returned by discovery")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "discover and show what would change, without writing")
	cmd.Flags().StringSliceVar(&f.tombstone, "tombstone", nil, "components to preserve across rebuilds when discovery no longer returns them (comma-separated)")
	return cmd
}
