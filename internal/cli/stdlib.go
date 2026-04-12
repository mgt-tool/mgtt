package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/sajonaro/mgtt/internal/providersupport"

	"github.com/spf13/cobra"
)

var stdlibCmd = &cobra.Command{
	Use:   "stdlib",
	Short: "Stdlib type operations",
}

var stdlibLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all stdlib types",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		renderStdlibLs(cmd.OutOrStdout())
		return nil
	},
}

var stdlibInspectCmd = &cobra.Command{
	Use:   "inspect <type>",
	Short: "Inspect a stdlib type",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if _, ok := providersupport.Stdlib[name]; !ok {
			return fmt.Errorf("stdlib type %q not found", name)
		}
		renderStdlibInspect(cmd.OutOrStdout(), name)
		return nil
	},
}

func init() {
	stdlibCmd.AddCommand(stdlibLsCmd)
	stdlibCmd.AddCommand(stdlibInspectCmd)
	rootCmd.AddCommand(stdlibCmd)
}

// stdlibOrder is the canonical display order for stdlib types.
var stdlibOrder = []string{
	"int", "float", "bool", "string",
	"duration", "bytes", "ratio", "percentage", "count", "timestamp",
}

// renderStdlibLs writes a summary table of all stdlib types to w.
func renderStdlibLs(w io.Writer) {
	for _, name := range stdlibOrder {
		dt, ok := providersupport.Stdlib[name]
		if !ok {
			continue
		}
		units := "~"
		if len(dt.Units) > 0 {
			units = strings.Join(dt.Units, "|")
		}
		rangeStr := formatRange(dt.Range)
		fmt.Fprintf(w, "  %-12s  base: %-7s  unit: %-20s  range: %s\n",
			name, dt.Base, units, rangeStr)
	}
}

// renderStdlibInspect writes detailed information about a single stdlib type to w.
func renderStdlibInspect(w io.Writer, name string) {
	dt, ok := providersupport.Stdlib[name]
	if !ok {
		fmt.Fprintf(w, "  stdlib type %q not found\n", name)
		return
	}

	fmt.Fprintf(w, "  name:  %s\n", dt.Name)
	fmt.Fprintf(w, "  base:  %s\n", dt.Base)

	if len(dt.Units) > 0 {
		fmt.Fprintf(w, "  units: %s\n", strings.Join(dt.Units, ", "))
	} else {
		fmt.Fprintf(w, "  units: ~\n")
	}

	fmt.Fprintf(w, "  range: %s\n", formatRange(dt.Range))
}

// formatRange returns a human-readable range string.
//   - nil Range → "~"
//   - Min only  → "0.."
//   - Both      → "0..1"
func formatRange(r *providersupport.Range) string {
	if r == nil {
		return "~"
	}
	if r.Min != nil && r.Max != nil {
		return fmt.Sprintf("%g..%g", *r.Min, *r.Max)
	}
	if r.Min != nil {
		return fmt.Sprintf("%g..", *r.Min)
	}
	if r.Max != nil {
		return fmt.Sprintf("..%g", *r.Max)
	}
	return "~"
}
