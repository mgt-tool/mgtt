package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/sajonaro/mgtt/internal/engine"
	"github.com/sajonaro/mgtt/internal/facts"
	"github.com/sajonaro/mgtt/internal/incident"
	"github.com/sajonaro/mgtt/internal/model"
	"github.com/sajonaro/mgtt/internal/providersupport"
	"github.com/sajonaro/mgtt/internal/providersupport/probe"
	probeexec "github.com/sajonaro/mgtt/internal/providersupport/probe/exec"
	"github.com/sajonaro/mgtt/internal/providersupport/probe/fixture"
	"github.com/sajonaro/mgtt/internal/state"

	"github.com/spf13/cobra"
)

var planModelPath string
var planComponent string

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Start guided troubleshooting",
	RunE:  runPlan,
}

func init() {
	planCmd.Flags().StringVar(&planModelPath, "model", "system.model.yaml", "path to system.model.yaml")
	planCmd.Flags().StringVar(&planComponent, "component", "", "start from a specific component instead of outermost")
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
	reg := providersupport.NewRegistry()
	for _, name := range providersupport.ListEmbedded() {
		p, err := providersupport.LoadEmbedded(name)
		if err == nil {
			reg.Register(p)
		}
	}

	// 3. Create executor (fixture or exec based on MGTT_FIXTURES).
	//    Build runner map from provider declarations unless in fixture mode.
	var executor probe.Executor
	runners := make(map[string]*probe.ExternalRunner)
	if fixturePath := os.Getenv("MGTT_FIXTURES"); fixturePath != "" {
		ex, err := fixture.Load(fixturePath)
		if err != nil {
			return fmt.Errorf("load fixtures: %w", err)
		}
		executor = ex
	} else {
		executor = probeexec.Default()
		for _, p := range reg.All() {
			if p.Meta.Command != "" {
				cmd := resolveCommand(p.Meta.Command, p.Meta.Name)
				runners[p.Meta.Name] = probe.NewExternalRunner(cmd)
			}
		}
	}

	// 4. Load fact store from active incident, or fall back to in-memory.
	var store *facts.Store
	inc, incErr := incident.Current()
	if incErr == nil && inc.Store != nil {
		store = inc.Store
	} else {
		store = facts.NewInMemory()
	}

	interactive := isInteractive()

	// 5. Plan loop.
	entry := m.EntryPoint()
	if planComponent != "" {
		entry = planComponent
	}
	renderPlanHeader(w, entry)

	for iteration := 0; iteration < 50; iteration++ { // safety limit
		tree := engine.Plan(m, reg, store, entry)

		renderPlanSuggestion(w, tree)

		// Check termination conditions.
		if tree.Suggested == nil {
			// No more probes. If we have a root cause, show it.
			if tree.RootCause != "" {
				renderRootCauseSummary(w, tree)
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

		var result probe.Result
		comp := m.Components[s.Component]

		// Use the external runner if this component's provider declares one.
		if comp != nil {
			if runner, ok := runners[s.Provider]; ok {
				vars := map[string]string{
					"namespace": m.Meta.Vars["namespace"],
					"type":      comp.Type,
				}
				result, err = runner.Probe(context.Background(), s.Component, s.Fact, vars)
			} else {
				rendered := probe.Substitute(s.Command, s.Component, m.Meta.Vars, nil)
				if err := probe.ValidateCommand(rendered, s.Command); err != nil {
					fmt.Fprintf(w, "\n  probe rejected: %v\n", err)
					break
				}
				result, err = executor.Run(context.Background(), probe.Command{
					Raw:       rendered,
					Parse:     s.ParseMode,
					Provider:  s.Provider,
					Component: s.Component,
					Fact:      s.Fact,
				})
			}
		} else {
			rendered := probe.Substitute(s.Command, s.Component, m.Meta.Vars, nil)
			if err := probe.ValidateCommand(rendered, s.Command); err != nil {
				fmt.Fprintf(w, "\n  probe rejected: %v\n", err)
				break
			}
			result, err = executor.Run(context.Background(), probe.Command{
				Raw:       rendered,
				Parse:     s.ParseMode,
				Provider:  s.Provider,
				Component: s.Component,
				Fact:      s.Fact,
			})
		}
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
		if store.IsDiskBacked() {
			if err := store.Save(); err != nil {
				fmt.Fprintf(w, "\n  warning: could not save state: %v\n", err)
			}
		}

		// Determine health for display: re-derive state after adding fact.
		derivation := state.Derive(m, reg, store)
		compState := derivation.ComponentStates[s.Component]
		comp = m.Components[s.Component]
		defaultActive := resolveDefaultActiveForCLI(comp, m.Meta.Providers, reg)
		healthy := compState == defaultActive && defaultActive != ""

		renderProbeResult(w, s.Component, s.Fact, result.Parsed, healthy)
	}

	return nil
}

// resolveCommand substitutes $MGTT_PROVIDER_DIR in a command string with the
// actual provider directory path.
func resolveCommand(command, providerName string) string {
	providerDir := ""
	if home := os.Getenv("MGTT_HOME"); home != "" {
		providerDir = filepath.Join(home, "providers", providerName)
	}
	if providerDir == "" {
		providerDir = filepath.Join("providers", providerName)
	}
	return strings.ReplaceAll(command, "$MGTT_PROVIDER_DIR", providerDir)
}

// resolveDefaultActiveForCLI looks up the default_active_state for a component.
func resolveDefaultActiveForCLI(comp *model.Component, metaProviders []string, reg *providersupport.Registry) string {
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

// renderPlanHeader renders the initial entry point message.
func renderPlanHeader(w io.Writer, entry string) {
	fmt.Fprintf(w, "\n  starting from outermost component: %s\n", entry)
}

// renderPlanSuggestion renders the current state of the path tree and the
// suggested next probe to w.
func renderPlanSuggestion(w io.Writer, tree *engine.PathTree) {
	// Show surviving paths.
	if len(tree.Paths) > 0 {
		fmt.Fprintf(w, "\n  %s to investigate:\n", pluralize(len(tree.Paths), "path", "paths"))
		for _, p := range tree.Paths {
			fmt.Fprintf(w, "  %-8s %s\n", p.ID, strings.Join(p.Components, " <- "))
		}
	}

	// Show eliminated paths.
	if len(tree.Eliminated) > 0 {
		fmt.Fprintln(w)
		for _, p := range tree.Eliminated {
			fmt.Fprintf(w, "  %-8s %s  (eliminated: %s)\n", p.ID, strings.Join(p.Components, " <- "), p.Reason)
		}
	}

	// Show suggested probe.
	if tree.Suggested != nil {
		s := tree.Suggested
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  -> probe %s %s\n", s.Component, s.Fact)
		var meta []string
		if s.Cost != "" {
			meta = append(meta, "cost: "+s.Cost)
		}
		if s.Access != "" {
			meta = append(meta, s.Access)
		}
		if len(s.Eliminates) > 0 {
			meta = append(meta, "eliminates "+strings.Join(s.Eliminates, ", ")+" if healthy")
		}
		if len(meta) > 0 {
			fmt.Fprintf(w, "     %s\n", strings.Join(meta, " | "))
		}
	}
}

// renderProbeResult renders the result of a single probe execution.
func renderProbeResult(w io.Writer, component, fact string, value any, healthy bool) {
	mark := checkmark(healthy)
	label := "healthy"
	if !healthy {
		label = "unhealthy"
	}
	fmt.Fprintf(w, "\n  %s %s.%s = %v   %s %s\n", checkmark(true), component, fact, value, mark, label)
}

// renderRootCauseSummary renders the final root cause determination.
func renderRootCauseSummary(w io.Writer, tree *engine.PathTree) {
	if tree.RootCause == "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  All components healthy -- no root cause found.")
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Root cause: %s\n", tree.RootCause)

	// Show the root cause path.
	for _, p := range tree.Paths {
		last := p.Components[len(p.Components)-1]
		if last == tree.RootCause {
			fmt.Fprintf(w, "  Path:       %s\n", strings.Join(p.Components, " <- "))
			break
		}
	}

	// Show state.
	if tree.States != nil {
		if st, ok := tree.States.ComponentStates[tree.RootCause]; ok {
			fmt.Fprintf(w, "  State:      %s\n", st)
		}
	}

	// Show eliminated components (only those NOT on surviving paths).
	if len(tree.Eliminated) > 0 {
		surviving := map[string]bool{}
		for _, p := range tree.Paths {
			for _, c := range p.Components {
				surviving[c] = true
			}
		}
		var names []string
		seen := map[string]bool{}
		for _, p := range tree.Eliminated {
			last := p.Components[len(p.Components)-1]
			if !seen[last] && !surviving[last] {
				seen[last] = true
				names = append(names, last)
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(w, "  Eliminated: %s\n", strings.Join(names, ", "))
		}
	}
}
