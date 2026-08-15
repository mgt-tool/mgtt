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

	"github.com/mgt-tool/mgtt/internal/expr"
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
}

// runModelBuild is the testable core: no cobra, no globals. Reads
// the existing model (if present), invokes every installed provider's
// discover, builds + diffs + gates + writes. Returns an exit code.
func runModelBuild(ctx context.Context, f modelBuildFlags, stdout, stderr io.Writer) int {
	snapshots, failures, homeErr := providersupport.DiscoverAll(ctx, f.mgttHome)
	if homeErr != nil {
		fmt.Fprintf(stderr, "cannot read providers dir: %v\n", homeErr)
		return 1
	}
	reportDiscoverFailures(stderr, failures)

	next, err := build.BuildModel(snapshots)
	if err != nil {
		fmt.Fprintf(stderr, "build model: %v\n", err)
		return 1
	}
	prev, code := loadPrevModel(f.output, stderr)
	if code != 0 {
		return code
	}
	mergePrev(prev, next, f.tombstone)

	diff := build.ComputeDiff(prev, next)
	if code := gateDeletions(stderr, diff, f); code != 0 {
		return code
	}
	renderBuildSummary(stdout, snapshots, next, diff)
	return writeBuiltModel(stdout, stderr, f.output, next)
}

// reportDiscoverFailures prints per-provider "no Discover() support" lines
// in deterministic (sorted) order.
func reportDiscoverFailures(w io.Writer, failures map[string]error) {
	keys := make([]string, 0, len(failures))
	for name := range failures {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		fmt.Fprintf(w, "  %s provider → no Discover() support (skipped): %v\n", name, failures[name])
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

// mergePrev carries tombstoned components AND hand-authored augmentations
// (healthy, failure_modes, vars, while-guards) from prev onto next.
// Discovery only returns structural facts — operators' semantic work on
// kept components must survive a rebuild.
func mergePrev(prev, next *model.Model, tombstone []string) {
	if prev == nil {
		return
	}
	for _, name := range tombstone {
		pc, ok := prev.Components[name]
		if !ok {
			continue
		}
		if _, alreadyInNext := next.Components[name]; alreadyInNext {
			continue
		}
		next.Components[name] = pc
	}
	mergeHandAuthored(prev, next)
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

// mergeHandAuthored copies hand-authored fields (HealthyRaw,
// FailureModes, Vars, plus while-guards on matching deps) from prev
// onto next's components. Discovery only knows structural facts —
// type / resource / depends targets — so any semantic augmentation
// lives on prev and must survive rebuild.
func mergeHandAuthored(prev, next *model.Model) {
	for name, nextComp := range next.Components {
		prevComp, ok := prev.Components[name]
		if !ok {
			continue
		}
		if len(nextComp.HealthyRaw) == 0 && len(prevComp.HealthyRaw) > 0 {
			nextComp.HealthyRaw = append([]string(nil), prevComp.HealthyRaw...)
			nextComp.Healthy = append([]expr.Node(nil), prevComp.Healthy...)
		}
		if len(nextComp.FailureModes) == 0 && len(prevComp.FailureModes) > 0 {
			nextComp.FailureModes = make(map[string][]string, len(prevComp.FailureModes))
			for k, v := range prevComp.FailureModes {
				nextComp.FailureModes[k] = append([]string(nil), v...)
			}
		}
		if len(nextComp.Vars) == 0 && len(prevComp.Vars) > 0 {
			nextComp.Vars = make(map[string]string, len(prevComp.Vars))
			for k, v := range prevComp.Vars {
				nextComp.Vars[k] = v
			}
		}
		// Port while-guards onto matching next-side deps (matched by
		// identical On target set). Discovery has no way to express
		// while-guards; if prev's dep matches next's by target, the
		// operator's guard applies to the merged edge.
		for i, nd := range nextComp.Depends {
			for _, pd := range prevComp.Depends {
				if pd.WhileRaw == "" || !sameTargets(nd.On, pd.On) {
					continue
				}
				nextComp.Depends[i].WhileRaw = pd.WhileRaw
				nextComp.Depends[i].While = pd.While
				break
			}
		}
	}
}

// sameTargets reports whether two depends-on lists target the same
// components (order-insensitive, duplicates collapsed).
func sameTargets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]struct{}, len(a))
	for _, s := range a {
		seen[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := seen[s]; !ok {
			return false
		}
	}
	return true
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
	cmd.Flags().StringSliceVar(&f.tombstone, "tombstone", nil, "components to preserve across rebuilds when discovery no longer returns them (comma-separated)")
	return cmd
}
