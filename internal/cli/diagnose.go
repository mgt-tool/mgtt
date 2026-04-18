package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

type diagnoseFlags struct {
	modelPath    string
	suspect      []string
	readonlyOnly bool
	maxProbes    int
	deadline     time.Duration
	onWrite      string
}

func newDiagnoseCmd() *cobra.Command {
	var f diagnoseFlags
	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Autopilot troubleshooting — run probes until root cause is found",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiagnose(cmd, f)
		},
	}
	cmd.Flags().StringVar(&f.modelPath, "model", "", "path to model.yaml (default: auto-detect in cwd)")
	cmd.Flags().StringSliceVar(&f.suspect, "suspect", nil, "comma-separated components (or component.state) that seem broken — soft prior, not a filter")
	cmd.Flags().BoolVar(&f.readonlyOnly, "readonly-only", true, "only run probes whose provider declares read_only: true")
	cmd.Flags().IntVar(&f.maxProbes, "max-probes", 20, "probe budget")
	cmd.Flags().DurationVar(&f.deadline, "deadline", 5*time.Minute, "wall-clock deadline")
	cmd.Flags().StringVar(&f.onWrite, "on-write", "pause", "behavior when a write-probe is next: pause|run|fail")
	return cmd
}

func init() {
	rootCmd.AddCommand(newDiagnoseCmd())
}

func runDiagnose(cmd *cobra.Command, f diagnoseFlags) error {
	return fmt.Errorf("not implemented")
}
