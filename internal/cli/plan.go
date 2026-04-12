package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"mgtt/internal/engine"
	"mgtt/internal/facts"
	"mgtt/internal/model"
	"mgtt/internal/probe"
	"mgtt/internal/probe/fixture"
	"mgtt/internal/provider"
	"mgtt/internal/render"
	"mgtt/internal/state"

	"github.com/spf13/cobra"
)

var planModelPath string

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Start guided troubleshooting",
	RunE:  runPlan,
}

func init() {
	planCmd.Flags().StringVar(&planModelPath, "model", "system.model.yaml", "path to system.model.yaml")
	rootCmd.AddCommand(planCmd)
}

// isInteractive returns true if stdin is a real terminal (not a pipe,
// /dev/null, or redirected file). It uses the TIOCGWINSZ ioctl to check
// whether stdin refers to a terminal device with a window size.
func isInteractive() bool {
	type winsize struct {
		Row, Col, Xpixel, Ypixel uint16
	}
	var ws winsize
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		os.Stdin.Fd(),
		syscall.TIOCGWINSZ,
		uintptr(unsafe.Pointer(&ws)),
	)
	return errno == 0
}

func runPlan(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	// 1. Load model.
	m, err := model.Load(planModelPath)
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}

	// 2. Load providers (embedded).
	reg := provider.NewRegistry()
	for _, name := range provider.ListEmbedded() {
		p, err := provider.LoadEmbedded(name)
		if err == nil {
			reg.Register(p)
		}
	}

	// 3. Create executor (fixture or exec based on MGTT_FIXTURES).
	var executor probe.Executor
	if fixturePath := os.Getenv("MGTT_FIXTURES"); fixturePath != "" {
		ex, err := fixture.Load(fixturePath)
		if err != nil {
			return fmt.Errorf("load fixtures: %w", err)
		}
		executor = ex
	} else {
		return fmt.Errorf("live probe execution not yet implemented; set MGTT_FIXTURES to use fixture backend")
	}

	// 4. Create fact store (in-memory for now; incident integration is separate).
	store := facts.NewInMemory()

	interactive := isInteractive()

	// 5. Plan loop.
	entry := m.EntryPoint()
	render.PlanHeader(w, entry)

	for iteration := 0; iteration < 50; iteration++ { // safety limit
		tree := engine.Plan(m, reg, store, "")

		render.PlanSuggestion(w, tree)

		// Check termination conditions.
		if tree.Suggested == nil {
			// No more probes. If we have a root cause, show it.
			if tree.RootCause != "" {
				render.RootCauseSummary(w, tree)
			} else {
				// All paths eliminated or no probes left.
				fmt.Fprintln(w)
				fmt.Fprintln(w, "  All components healthy -- no root cause found.")
			}
			break
		}

		// Prompt for acceptance (auto-accept if non-interactive).
		if interactive {
			fmt.Fprintf(w, "\n  run probe? [Y/n] ")
			reader := bufio.NewReader(os.Stdin)
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(strings.ToLower(line))
			if line == "n" || line == "no" {
				fmt.Fprintln(w, "  skipped.")
				break
			}
		}

		// Build and run probe.
		s := tree.Suggested
		rendered := probe.Substitute(s.Command, s.Component, m.Meta.Vars, nil)

		result, err := executor.Run(context.Background(), probe.Command{
			Raw:       rendered,
			Parse:     s.ParseMode,
			Provider:  s.Provider,
			Component: s.Component,
			Fact:      s.Fact,
		})
		if err != nil {
			fmt.Fprintf(w, "\n  probe error: %v\n", err)
			break
		}

		// Store the fact.
		store.Append(s.Component, facts.Fact{
			Key:       s.Fact,
			Value:     result.Parsed,
			Collector: "probe",
			At:        time.Now(),
			Raw:       result.Raw,
		})

		// Determine health for display: re-derive state after adding fact.
		derivation := state.Derive(m, reg, store)
		compState := derivation.ComponentStates[s.Component]
		comp := m.Components[s.Component]
		defaultActive := resolveDefaultActiveForCLI(comp, m.Meta.Providers, reg)
		healthy := compState == defaultActive && defaultActive != ""

		render.ProbeResult(w, s.Component, s.Fact, result.Parsed, healthy)
	}

	return nil
}

// resolveDefaultActiveForCLI looks up the default_active_state for a component.
func resolveDefaultActiveForCLI(comp *model.Component, metaProviders []string, reg *provider.Registry) string {
	providers := comp.Providers
	if len(providers) == 0 {
		providers = metaProviders
	}
	t, _, err := reg.ResolveType(providers, comp.Type)
	if err != nil {
		return ""
	}
	return t.DefaultActiveState
}
