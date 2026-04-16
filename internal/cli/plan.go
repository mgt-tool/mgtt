package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mgt-tool/mgtt/internal/engine"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
	probeexec "github.com/mgt-tool/mgtt/internal/providersupport/probe/exec"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe/fixture"
	"github.com/mgt-tool/mgtt/internal/state"

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

// isInteractive reports whether stdin is attached to a terminal.
func isInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func runPlan(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	// 1. Load model.
	m, err := model.Load(planModelPath)
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}

	reg := providersupport.LoadAllForUse()

	executor, err := buildExecutor(reg)
	if err != nil {
		return err
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

	entry := m.EntryPoint()
	if planComponent != "" {
		entry = planComponent
	}
	renderPlanHeader(w, entry)

	// 50 is well above any realistic fact count; the guard catches pathological
	// models where the engine keeps suggesting probes forever.
	for iteration := 0; iteration < 50; iteration++ {
		tree := engine.Plan(m, reg, store, entry)

		renderPlanSuggestion(w, tree)

		if tree.Suggested == nil {
			if tree.RootCause != "" {
				renderRootCauseSummary(w, tree)
			} else {
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

		s := tree.Suggested
		comp := m.Components[s.Component]
		compType := ""
		if comp != nil {
			compType = comp.Type
		}

		rendered := probe.Substitute(s.Command, s.Component, m.Meta.Vars, nil)
		if err := probe.ValidateCommand(rendered, s.Command); err != nil {
			fmt.Fprintf(w, "\n  probe rejected: %v\n", err)
			break
		}
		ctx := probe.WithTracer(context.Background(), probe.NewTracer())
		result, err := executor.Run(ctx, probe.Command{
			Raw:       rendered,
			Parse:     s.ParseMode,
			Provider:  s.Provider,
			Component: s.Component,
			Fact:      s.Fact,
			Type:      compType,
			Vars:      m.Meta.Vars,
			Timeout:   probeTimeout(),
		})
		if err != nil {
			fmt.Fprintf(w, "\n  probe error: %v\n", err)
			break
		}

		// not_found: the underlying resource is missing. Surface it to the
		// operator AND record the fact with a nil value so the engine's expr
		// layer produces an UnresolvedError on the next iteration — preventing
		// the planner from suggesting the same probe in a loop.
		if result.Status == probe.StatusNotFound {
			fmt.Fprintf(w, "\n  resource not found: %s.%s\n", s.Component, s.Fact)
			store.Append(s.Component, facts.Fact{
				Key:       s.Fact,
				Value:     nil,
				Collector: "probe",
				At:        time.Now(),
				Note:      "not_found",
			})
			if store.IsDiskBacked() {
				_ = store.Save()
			}
			continue
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
		defaultActive := engine.ResolveDefaultActive(comp, m.Meta.Providers, reg)
		healthy := compState == defaultActive && defaultActive != ""

		renderProbeResult(w, s.Component, s.Fact, result.Parsed, healthy)
	}

	return nil
}

// probeTimeout reads MGTT_PROBE_TIMEOUT (e.g. "60s", "2m") and returns it
// as a time.Duration. Returns 0 (= use the runner's default 30s) when the
// var is unset.
//
// Unparseable values emit a one-time stderr warning and fall back to the
// default — operators who set "60" (no unit) discover their config is wrong
// instead of silently getting the 30s they thought they overrode.
func probeTimeout() time.Duration {
	v := os.Getenv("MGTT_PROBE_TIMEOUT")
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		probeTimeoutWarnOnce.Do(func() {
			fmt.Fprintf(os.Stderr,
				"[mgtt] MGTT_PROBE_TIMEOUT=%q is not a valid duration (e.g. '60s', '2m'); using default\n", v)
		})
		return 0
	}
	return d
}

var probeTimeoutWarnOnce sync.Once

// buildExecutor selects a probe executor based on MGTT_FIXTURES. In fixture
// mode, all probes go through the fixture executor. Otherwise the shell
// executor is used, with any provider runner binaries mixed in via Mux.
func buildExecutor(reg *providersupport.Registry) (probe.Executor, error) {
	if fixturePath := os.Getenv("MGTT_FIXTURES"); fixturePath != "" {
		ex, err := fixture.Load(fixturePath)
		if err != nil {
			return nil, fmt.Errorf("load fixtures: %w", err)
		}
		return ex, nil
	}

	runners := map[string]probe.Executor{}
	for _, p := range reg.All() {
		if p.Meta.Command == "" {
			continue
		}
		// Registry was built via LoadAllForUse at the call site, so
		// CheckCompatible has already been run. No need to re-gate here.
		runners[p.Meta.Name] = probe.NewExternalRunner(resolveCommand(p.Meta.Command, p.Meta.Name))
	}
	if len(runners) == 0 {
		return probeexec.Default(), nil
	}
	return &probe.Mux{Default: probeexec.Default(), Runners: runners}, nil
}

// resolveCommand substitutes $MGTT_PROVIDER_DIR in a command string.
func resolveCommand(command, providerName string) string {
	dir := providersupport.ProviderDir(providerName)
	if dir == "" {
		dir = filepath.Join("providers", providerName)
	}
	return strings.ReplaceAll(command, "$MGTT_PROVIDER_DIR", dir)
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

	if names := engine.EliminatedOnly(tree); len(names) > 0 {
		fmt.Fprintf(w, "  Eliminated: %s\n", strings.Join(names, ", "))
	}
}
