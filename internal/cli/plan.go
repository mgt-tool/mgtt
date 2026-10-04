// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mgt-tool/mgtt/internal/engine"
	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe/dispatch"
	"github.com/mgt-tool/mgtt/internal/state"

	"github.com/spf13/cobra"
)

type planFlags struct {
	modelPath string
	component string
}

func newPlanCmd() *cobra.Command {
	f := &planFlags{}
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Start guided troubleshooting",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlan(cmd, f, args)
		},
	}
	cmd.Flags().StringVar(&f.modelPath, "model", "system.model.yaml", "path to system.model.yaml")
	cmd.Flags().StringVar(&f.component, "component", "", "start from a specific component instead of outermost")
	return cmd
}

func init() {
	registerCommand(newPlanCmd)
}

// isInteractive reports whether stdin is attached to a terminal.
func isInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func runPlan(cmd *cobra.Command, f *planFlags, args []string) error {
	w := cmd.OutOrStdout()
	pc, err := loadPlanContext(f)
	if err != nil {
		return err
	}
	renderPlanHeader(w, pc.entry)

	// maxPlanIterations is well above any realistic fact count; the guard
	// catches pathological models where the engine keeps suggesting
	// probes forever.
	const maxPlanIterations = 50
	// Forward any persisted suspect hints from the active incident so
	// plan biases identically to MCP. Empty when no incident is active.
	suspects := strategy.ParseSuspectHints(pc.store.Meta.Suspects)
	interactive := isInteractive()
	for range maxPlanIterations {
		tree := engine.PlanWith(pc.m, pc.reg, pc.store, pc.entry, suspects)
		renderPlanSuggestion(w, tree)
		if tree.Suggested == nil {
			if tree.RootCause != "" {
				renderRootCauseSummary(w, tree)
			} else {
				fmt.Fprintln(w)
				fmt.Fprintln(w, "  All components healthy -- no root cause found.")
			}
			return nil
		}
		if interactive && !promptProbeAccept(w) {
			fmt.Fprintln(w, "  skipped.")
			return nil
		}
		if stop := runPlanProbe(w, pc.m, pc.reg, pc.store, pc.executor, tree.Suggested); stop {
			return nil
		}
	}
	return nil
}

// planContext bundles the loaded model/registry/executor/store and the
// resolved entry component for one `mgtt plan` invocation.
type planContext struct {
	m        *model.Model
	reg      *providersupport.Registry
	executor probe.Executor
	store    *facts.Store
	entry    string
}

func loadPlanContext(f *planFlags) (*planContext, error) {
	m, err := model.Load(f.modelPath)
	if err != nil {
		return nil, fmt.Errorf("load model: %w", err)
	}
	reg, err := loadRegistryForUse()
	if err != nil {
		return nil, err
	}
	if err := resolveModelProviders(m, os.Stderr); err != nil {
		return nil, err
	}
	executor, err := dispatch.New(reg)
	if err != nil {
		return nil, err
	}
	entry := m.EntryPoint()
	if f.component != "" {
		entry = f.component
	}
	return &planContext{
		m:        m,
		reg:      reg,
		executor: executor,
		store:    incident.CurrentStoreOrMemory(),
		entry:    entry,
	}, nil
}

// runPlanProbe renders and executes a single suggested probe, updating
// the store with its result. Returns true when the loop should stop
// (error or probe rejected); false when the caller should keep going.
func runPlanProbe(w io.Writer, m *model.Model, reg *providersupport.Registry, store *facts.Store, executor probe.Executor, s *engine.Probe) (stop bool) {
	rendered := probe.Substitute(s.Command, s.Component, s.Vars, nil)
	if err := probe.ValidateCommand(rendered, s.Command); err != nil {
		fmt.Fprintf(w, "\n  probe rejected: %v\n", err)
		return true
	}
	// strategy.Probe already carries resolved Type, Resource, and merged
	// Vars — no reach-back into m.Components needed.
	ctx := probe.WithTracer(context.Background(), probe.NewTracer())
	result, err := executor.Run(ctx, probe.Command{
		Raw:       rendered,
		Parse:     s.ParseMode,
		Provider:  s.Provider,
		Component: s.Component,
		Fact:      s.Fact,
		Type:      s.Type,
		Resource:  s.Resource,
		Vars:      s.Vars,
		Timeout:   probeTimeout(),
	})
	outcome, err := dispatch.Record(store, s.Component, s.Fact, result, err)
	if err != nil {
		fmt.Fprintf(w, "\n  probe error: %v\n", err)
		return true
	}
	saveProbeStore(store, w)
	if outcome != dispatch.Value {
		// Recorded as unknown: the expr layer reads it as unresolved on
		// the next iteration, so the planner moves on instead of looping.
		fmt.Fprintf(w, "\n  %s.%s: %s\n", s.Component, s.Fact, strings.ReplaceAll(string(outcome), "_", " "))
		return false
	}

	derivation := state.Derive(m, reg, store)
	defaultActive := engine.ResolveDefaultActive(m.Components[s.Component], m, reg)
	healthy := derivation.ComponentStates[s.Component] == defaultActive && defaultActive != ""
	renderProbeResult(w, s.Component, s.Fact, result.Parsed, healthy)
	return false
}

// saveProbeStore persists a disk-backed store after a probe outcome.
func saveProbeStore(store *facts.Store, w io.Writer) {
	if !store.IsDiskBacked() {
		return
	}
	if err := store.Save(); err != nil {
		fmt.Fprintf(w, "\n  warning: could not save state: %v\n", err)
	}
}

// promptProbeAccept asks the operator to accept the suggested probe.
// Returns false on an explicit "n" / "no"; any other input (including
// read failure) accepts.
func promptProbeAccept(w io.Writer) bool {
	fmt.Fprintf(w, "\n  run probe? [Y/n] ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line != "n" && line != "no"
}

// resolveModelProviders parses and resolves the model's providers: list against
// the locally-installed set. Warnings (legacy bare-name refs) are printed to
// errW. Returns an error if any ref cannot be resolved.
func resolveModelProviders(m *model.Model, errW io.Writer) error {
	if len(m.Meta.Providers) == 0 {
		return nil
	}

	// Parse each provider ref string.
	refs := make([]model.ProviderRef, 0, len(m.Meta.Providers))
	for _, entry := range m.Meta.Providers {
		ref, err := model.ParseProviderRef(entry)
		if err != nil {
			return fmt.Errorf("model provider ref %q: %w", entry, err)
		}
		refs = append(refs, ref)
	}

	// Build InstalledProvider list from disk.
	installed := buildInstalledList()

	// Resolve.
	_, warnings, err := model.Resolve(refs, installed)

	// Print warnings (legacy bare-name refs) to stderr.
	for _, w := range warnings {
		fmt.Fprintf(errW, "⚠ %s\n", w.Message)
	}

	return err
}

// buildInstalledList constructs the model.InstalledProvider list from all
// providers found on disk via ListEmbedded.
func buildInstalledList() []model.InstalledProvider {
	names := providersupport.ListEmbedded()
	var installed []model.InstalledProvider
	for _, name := range names {
		dir := providersupport.ProviderDir(name)
		if dir == "" {
			continue
		}
		meta, _ := providersupport.ReadInstallMeta(dir)
		p, err := providersupport.LoadFromDir(dir)
		if err != nil {
			continue
		}
		installed = append(installed, model.InstalledProvider{
			Name:      name,
			Namespace: meta.Namespace,
			Version:   p.Meta.Version,
			Dir:       dir,
		})
	}
	return installed
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
	defer func() {
		for _, u := range tree.CannotRuleOut {
			fmt.Fprintf(w, "  Cannot rule out: %s\n", u)
		}
		for _, d := range tree.RedundancyDegraded {
			fmt.Fprintf(w, "  Redundancy degraded: %s\n", d)
		}
	}()
	if tree.RootCause == "" {
		fmt.Fprintln(w)
		if len(tree.CannotRuleOut) > 0 {
			fmt.Fprintln(w, "  No root cause among the components that could be seen.")
		} else if len(tree.RedundancyDegraded) > 0 {
			fmt.Fprintln(w, "  No root cause: redundancy is absorbing the failures below.")
		} else {
			fmt.Fprintln(w, "  All components healthy -- no root cause found.")
		}
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
