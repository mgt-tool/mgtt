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
	// Invoke providers.
	snapshots, failures, homeErr := providersupport.DiscoverAll(ctx, f.mgttHome)
	if homeErr != nil {
		fmt.Fprintf(stderr, "cannot read providers dir: %v\n", homeErr)
		return 1
	}
	// Sort failure keys so warnings come out deterministically — Task 9
	// reviewer flagged that map iteration order is random.
	failureKeys := make([]string, 0, len(failures))
	for name := range failures {
		failureKeys = append(failureKeys, name)
	}
	sort.Strings(failureKeys)
	for _, name := range failureKeys {
		fmt.Fprintf(stderr, "  %s provider → no Discover() support (skipped): %v\n", name, failures[name])
	}

	// Merge snapshots.
	next, err := build.BuildModel(snapshots)
	if err != nil {
		fmt.Fprintf(stderr, "build model: %v\n", err)
		return 1
	}

	// Load prev model (if committed).
	var prev *model.Model
	if _, statErr := os.Stat(f.output); statErr == nil {
		prev, err = model.Load(f.output)
		if err != nil {
			fmt.Fprintf(stderr, "load existing %s: %v\n", f.output, err)
			return 1
		}
	}

	// Carry tombstoned components forward from prev. --tombstone means
	// "this component is still there, just not discovered right now"
	// (e.g. air-gapped infra, partial discovery failure) — preserve it
	// verbatim so the written model is the merged reality.
	if prev != nil && len(f.tombstone) > 0 {
		for _, name := range f.tombstone {
			pc, ok := prev.Components[name]
			if !ok {
				continue // nothing to preserve — unknown tombstone name
			}
			if _, alreadyInNext := next.Components[name]; alreadyInNext {
				continue // discovery did return it — use the fresh version
			}
			next.Components[name] = pc
		}
	}

	// Diff + gate.
	diff := build.ComputeDiff(prev, next)
	if err := build.GateDeletions(diff, build.GateFlags{
		AllowDeletes: f.allowDeletes,
		Tombstone:    f.tombstone,
	}); err != nil {
		if errors.Is(err, build.ErrDeletionsRefused) {
			fmt.Fprintln(stderr, "Model drift detected (vs committed "+f.output+"):")
			for _, rm := range diff.Removed {
				fmt.Fprintf(stderr, "  -  %s\n", rm)
			}
			fmt.Fprintln(stderr)
			// Unwrap to get the options-hint body; the sentinel prefix
			// plus `: ` was prepended by GateDeletions' Errorf.
			body := strings.TrimPrefix(err.Error(), build.ErrDeletionsRefused.Error()+": ")
			fmt.Fprintln(stderr, body)
			return 1
		}
		fmt.Fprintf(stderr, "gate: %v\n", err)
		return 1
	}

	// Summary (always printed).
	// Sort provider names so the summary is deterministic.
	snapKeys := make([]string, 0, len(snapshots))
	for k := range snapshots {
		snapKeys = append(snapKeys, k)
	}
	sort.Strings(snapKeys)
	for _, name := range snapKeys {
		snap := snapshots[name]
		fmt.Fprintf(stdout, "  %s provider    → %d components, %d dependencies\n", name, len(snap.Components), len(snap.Dependencies))
	}
	fmt.Fprintf(stdout, "\n  Model: %d components\n", len(next.Components))
	if len(diff.Added) > 0 {
		fmt.Fprintf(stdout, "  Added:   %s\n", strings.Join(diff.Added, ", "))
	}
	if len(diff.Removed) > 0 {
		fmt.Fprintf(stdout, "  Removed: %s\n", strings.Join(diff.Removed, ", "))
	}

	// Write.
	outFile, err := os.Create(f.output)
	if err != nil {
		fmt.Fprintf(stderr, "open output: %v\n", err)
		return 1
	}
	defer outFile.Close()
	if err := build.EmitYAML(next, outFile); err != nil {
		fmt.Fprintf(stderr, "emit: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "  Written: %s\n", f.output)
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
	cmd.Flags().StringSliceVar(&f.tombstone, "tombstone", nil, "components to preserve across rebuilds when discovery no longer returns them (comma-separated)")
	return cmd
}

func init() {
	modelCmd.AddCommand(newModelBuildCmd())
}
