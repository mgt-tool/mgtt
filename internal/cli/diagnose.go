// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
	"github.com/mgt-tool/mgtt/internal/scenarios"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type diagnoseFlags struct {
	modelPath    string
	suspect      []string
	readonlyOnly bool
	maxProbes    int
	deadline     time.Duration
	onWrite      string
}

// probeRunner executes a probe and returns a display-friendly outcome
// string. Tests swap in a stub that returns canned outcomes without
// shelling out. The production wiring (realProbeRunner) delegates to the
// same executor `mgtt plan` builds.
type probeRunner interface {
	Run(ctx context.Context, p *strategy.Probe, store *facts.Store) (string, error)
}

// Package-level override points so tests can replace runtime defaults
// without reaching through a constructor chain. Production `runDiagnose`
// picks these up on every call.
var (
	newProbeRunner           = defaultNewProbeRunner
	diagnoseStdin  io.Reader = os.Stdin
	diagnoseLoader           = defaultDiagnoseLoader
)

// defaultDiagnoseLoader resolves the model file and scenarios sidecar
// for the diagnose path. Tests replace diagnoseLoader (declared above)
// to inject synthetic fixtures without touching disk.
func defaultDiagnoseLoader(modelPathHint string) (*model.Model, *providersupport.Registry, []scenarios.Scenario, error) {
	modelPath, err := resolveModelPath(modelPathHint)
	if err != nil {
		return nil, nil, nil, err
	}
	return loadModelAndScenarios(modelPath)
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
	cmd.Flags().StringSliceVar(&f.suspect, "suspect", nil, "comma-separated components that seem broken — soft prior, not a filter. State forms: 'component', 'component/state' (preferred), 'component=state', 'component.state' (legacy; mis-parses names containing dots)")
	cmd.Flags().BoolVar(&f.readonlyOnly, "readonly-only", true, "only run probes whose provider declares read_only: true")
	cmd.Flags().IntVar(&f.maxProbes, "max-probes", 20, "probe budget")
	cmd.Flags().DurationVar(&f.deadline, "deadline", 5*time.Minute, "wall-clock deadline")
	cmd.Flags().StringVar(&f.onWrite, "on-write", "pause", "behavior when a write-probe is next: pause|run|fail")
	return cmd
}

func init() {
	registerCommand(newDiagnoseCmd)
}

// probeRecord is a single trail entry — what we probed and what we saw.
type probeRecord struct {
	probe   *strategy.Probe
	outcome string
}

func runDiagnose(cmd *cobra.Command, f diagnoseFlags) error {
	parentCtx := cmd.Context()
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, f.deadline)
	defer cancel()

	m, reg, scs, err := diagnoseLoader(f.modelPath)
	if err != nil {
		return err
	}

	runner, err := newProbeRunner(reg)
	if err != nil {
		return err
	}

	// Load the active incident's fact store when present — same as
	// `mgtt plan`. Lets operators pre-seed observations (e.g. `mgtt fact
	// add cloudflare operator_says_healthy true` in CI to anchor a
	// generic terminal) before autopilot starts.
	store := incident.CurrentStoreOrMemory()
	// Explicit --suspect wins; else inherit persisted hints from the
	// active incident (set at incident.start or MCP's IncidentStart).
	suspects := parseSuspectHints(f.suspect)
	if len(suspects) == 0 {
		suspects = strategy.ParseSuspectHints(store.Meta.Suspects)
	}
	start := time.Now()
	probesRun := 0

	// Non-TTY stdin + UNSEEDED generic components = infinite skip loop
	// (EOF on every prompt) that silently burns the probe budget. Reject
	// early only when a generic component has no pre-seeded facts — if
	// the operator already recorded operator_says_healthy via `mgtt fact
	// add`, diagnose will never prompt, so CI is fine.
	if !stdinLooksInteractive(diagnoseStdin) && modelUsesUnseededGenericComponent(m, reg, store) {
		return fmt.Errorf("mgtt diagnose requires an interactive terminal for generic components: stdin is not a TTY and at least one generic component has no pre-seeded facts; rerun from a terminal, pre-seed via `mgtt fact add <component> operator_says_healthy true`, or replace generic components with typed ones")
	}

	loop := &diagnoseLoop{
		cmd: cmd, f: &f, m: m, reg: reg, scs: scs,
		store: store, suspects: suspects, runner: runner,
		start: start,
	}
	for probesRun < f.maxProbes {
		if ctx.Err() != nil {
			reportPartial(cmd, loop.store, loop.trail, "deadline exceeded", probesRun, f.maxProbes, start, f.deadline)
			return nil
		}
		done, err := loop.step(ctx, &probesRun)
		if err != nil || done {
			return err
		}
	}
	reportPartial(cmd, loop.store, loop.trail, "budget exhausted", probesRun, f.maxProbes, start, f.deadline)
	return nil
}

// diagnoseLoop holds the state one runDiagnose iteration needs to
// read and mutate. Pulling it out of runDiagnose lets step() be a
// regular method with linear control flow instead of a 70-line inline
// block.
type diagnoseLoop struct {
	cmd      *cobra.Command
	f        *diagnoseFlags
	m        *model.Model
	reg      *providersupport.Registry
	scs      []scenarios.Scenario
	store    *facts.Store
	suspects []strategy.SuspectHint
	runner   probeRunner
	trail    []probeRecord
	start    time.Time
}

// step runs one iteration: suggest, check terminal, gate, execute,
// record. Returns done=true when the caller should stop the loop
// (success, stuck, or an early-exit report); err for hard failures.
// probesRun is incremented when the iteration actually burned budget.
func (l *diagnoseLoop) step(ctx context.Context, probesRun *int) (done bool, err error) {
	input := strategy.Input{Model: l.m, Registry: l.reg, Store: l.store, Scenarios: l.scs, Suspects: l.suspects}
	decision := strategy.AutoSelect(input).SuggestProbe(input)
	switch {
	case decision.Done:
		reportDone(l.cmd, l.m, decision.RootCause, l.store, l.trail, *probesRun, l.f.maxProbes, l.start, l.f.deadline, l.suspects)
		return true, nil
	case decision.Stuck:
		reportStuck(l.cmd, l.store, l.trail, *probesRun, l.f.maxProbes, l.start, l.f.deadline)
		return true, nil
	case decision.Probe == nil:
		reportPartial(l.cmd, l.store, l.trail, "strategy returned no probe", *probesRun, l.f.maxProbes, l.start, l.f.deadline)
		return true, nil
	}

	p := decision.Probe
	if isGenericComponent(l.m, l.reg, p.Component) {
		return l.handleGenericPrompt(p, probesRun)
	}
	if stop, err := l.checkReadonlyGate(p, probesRun); stop || err != nil {
		return stop, err
	}
	outcome, err := l.runner.Run(ctx, p, l.store)
	if err != nil {
		if ctx.Err() != nil {
			reportPartial(l.cmd, l.store, l.trail, "deadline exceeded", *probesRun, l.f.maxProbes, l.start, l.f.deadline)
			return true, nil
		}
		return false, fmt.Errorf("probe %s.%s: %w", p.Component, p.Fact, err)
	}
	l.trail = append(l.trail, probeRecord{probe: p, outcome: outcome})
	*probesRun++
	return false, nil
}

// handleGenericPrompt asks the operator about a generic component
// rather than shelling out. Stdin-closed mid-session records a skip
// sentinel (so Occam won't re-select the step) and reports partial.
func (l *diagnoseLoop) handleGenericPrompt(p *strategy.Probe, probesRun *int) (bool, error) {
	answer, err := promptYesNoSkip(l.cmd, p.Component)
	if err != nil {
		if err == errNoMoreAnswers {
			applyOperatorAnswer(l.store, p.Component, "skip")
			l.trail = append(l.trail, probeRecord{probe: p, outcome: "operator-answered: skip (stdin closed)"})
			reportPartial(l.cmd, l.store, l.trail, "no more operator input (stdin closed)", *probesRun+1, l.f.maxProbes, l.start, l.f.deadline)
			return true, nil
		}
		return false, err
	}
	applyOperatorAnswer(l.store, p.Component, answer)
	l.trail = append(l.trail, probeRecord{probe: p, outcome: fmt.Sprintf("operator-answered: %s", answer)})
	*probesRun++
	return false, nil
}

// checkReadonlyGate enforces --readonly-only. Returns stop=true when
// the gate ended the run (pause / fail), err for invalid --on-write.
func (l *diagnoseLoop) checkReadonlyGate(p *strategy.Probe, probesRun *int) (bool, error) {
	if !l.f.readonlyOnly || probeIsReadOnly(p, l.m, l.reg) {
		return false, nil
	}
	switch l.f.onWrite {
	case "pause":
		reportPartial(l.cmd, l.store, l.trail, fmt.Sprintf("next probe requires writes (component=%s fact=%s); --on-write=pause", p.Component, p.Fact), *probesRun, l.f.maxProbes, l.start, l.f.deadline)
		return true, nil
	case "fail":
		return false, fmt.Errorf("write probe encountered: %s.%s (--on-write=fail)", p.Component, p.Fact)
	case "run", "":
		return false, nil
	default:
		return false, fmt.Errorf("invalid --on-write value %q", l.f.onWrite)
	}
}

// resolveModelPath picks the model file. When the operator passed --model
// explicitly we trust them; otherwise look for model.yaml then
// system.model.yaml in the CWD so diagnose works in both conventions.
func resolveModelPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	for _, candidate := range []string{"model.yaml", "system.model.yaml"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no model found: pass --model <path> or run from a directory containing model.yaml")
}

// loadModelAndScenarios reads the model, builds the provider registry,
// resolves provider refs, and reads a sibling scenarios.yaml when one
// exists. Missing scenarios.yaml is not an error — AutoSelect falls back
// to BFS when Scenarios is nil.
func loadModelAndScenarios(path string) (*model.Model, *providersupport.Registry, []scenarios.Scenario, error) {
	m, err := model.Load(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load model: %w", err)
	}
	reg, err := loadRegistryForUse()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := resolveModelProviders(m, os.Stderr); err != nil {
		return nil, nil, nil, err
	}

	scs, _, err := scenarios.LoadSiblingOf(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load scenarios: %w", err)
	}
	return m, reg, scs, nil
}

// parseSuspectHints delegates to strategy.ParseSuspectHints — kept as a
// local alias so older call sites and tests don't churn.
func parseSuspectHints(raw []string) []strategy.SuspectHint {
	return strategy.ParseSuspectHints(raw)
}

// probeIsReadOnly resolves the provider that owns the probe and returns
// its read_only posture. Unknown providers default to "not read-only" —
// safer to pause-and-ask than to silently execute against an unvalidated
// plugin.
func probeIsReadOnly(p *strategy.Probe, m *model.Model, reg *providersupport.Registry) bool {
	if reg == nil || p == nil {
		return false
	}
	prov, ok := reg.Get(p.Provider)
	if !ok {
		return false
	}
	return prov.ReadOnly
}

// isGenericComponent returns true when the component resolves to the
// "generic" provider — the fallback provider whose probes are operator
// questions instead of shell commands.
func isGenericComponent(m *model.Model, reg *providersupport.Registry, compName string) bool {
	if m == nil || reg == nil {
		return false
	}
	comp := m.Components[compName]
	if comp == nil {
		return false
	}
	_, providerName, err := comp.ResolveType(m, reg)
	if err != nil {
		return false
	}
	return providerName == "generic"
}

// errNoMoreAnswers signals that the prompt reader ran out of input
// (EOF) before the operator could answer. The diagnose loop treats this
// as "skip forever" — record a skip-marker fact and bail out cleanly
// rather than loop on every subsequent generic prompt.
var errNoMoreAnswers = fmt.Errorf("no more operator input")

// promptYesNoSkip asks the operator whether a component looks healthy
// and returns one of "y", "n", or "skip". Any other input is an error so
// we don't silently eat a typo mid-incident. EOF on stdin (e.g. running
// under `mgtt diagnose < /dev/null`) returns errNoMoreAnswers so the
// caller can break out of the generic-component loop.
func promptYesNoSkip(cmd *cobra.Command, compName string) (string, error) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Is '%s' healthy? [y/n/skip]: ", compName)
	reader := bufio.NewReader(diagnoseStdin)
	line, err := reader.ReadString('\n')
	if err == io.EOF && strings.TrimSpace(line) == "" {
		// True end-of-input, nothing buffered — caller should stop
		// re-prompting rather than treat empty-EOF as "skip".
		return "", errNoMoreAnswers
	}
	if err != nil && err != io.EOF {
		return "", err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	switch answer {
	case "y", "yes":
		return "y", nil
	case "n", "no":
		return "n", nil
	case "skip", "s", "":
		return "skip", nil
	default:
		return "", fmt.Errorf("unrecognized answer %q (want y/n/skip)", answer)
	}
}

// operatorSkippedKey is the synthetic fact key recorded when an operator
// explicitly skips a generic-component prompt (or stdin closes during
// one). Occam's fact-level verified-gate recognises this key as
// "verified-but-skipped", so the same component+fact isn't re-selected
// on subsequent iterations.
const operatorSkippedKey = "__operator_skipped"

// applyOperatorAnswer records the operator's verdict as a fact so the
// strategy can prune downstream scenarios. "skip" records a marker fact
// under operatorSkippedKey so the strategy treats the step as verified-
// but-skipped and doesn't re-select it next iteration (which would
// silently burn --max-probes).
func applyOperatorAnswer(store *facts.Store, compName, answer string) {
	switch answer {
	case "y":
		store.Append(compName, facts.Fact{
			Key:       "operator_says_healthy",
			Value:     true,
			Collector: "operator",
			At:        time.Now(),
		})
	case "n":
		store.Append(compName, facts.Fact{
			Key:       "operator_says_healthy",
			Value:     false,
			Collector: "operator",
			At:        time.Now(),
		})
	case "skip":
		store.Append(compName, facts.Fact{
			Key:       operatorSkippedKey,
			Value:     true,
			Collector: "operator",
			At:        time.Now(),
			Note:      "operator skipped prompt",
		})
		// Also record the canonical fact the occam scenario is probing
		// so pickSymptomInward's fact-level gate treats this step as
		// verified. Without this, every loop iteration re-selects the
		// same generic component and the budget is burned on prompts
		// the operator has already declined.
		store.Append(compName, facts.Fact{
			Key:       "operator_says_healthy",
			Value:     nil,
			Collector: "operator",
			At:        time.Now(),
			Note:      "skipped",
		})
	}
}

// modelUsesUnseededGenericComponent returns true only when a generic
// component exists AND its operator_says_healthy fact has not been
// pre-seeded in store. Used by the non-TTY gate: if CI recorded the
// answer in advance, diagnose can run without prompting.
func modelUsesUnseededGenericComponent(m *model.Model, reg *providersupport.Registry, store *facts.Store) bool {
	if m == nil || reg == nil {
		return false
	}
	for name := range m.Components {
		if !isGenericComponent(m, reg, name) {
			continue
		}
		if store == nil || store.Latest(name, "operator_says_healthy") == nil {
			return true
		}
	}
	return false
}

// stdinLooksInteractive reports whether r appears safe to prompt on.
// The check returns true in two cases:
//   - r is *os.File and its fd passes term.IsTerminal (real TTY).
//   - r is a non-*os.File reader swapped in by tests or an embedder.
//     In that case the caller is driving the stream deliberately and
//     we defer to errNoMoreAnswers handling on EOF rather than refuse
//     upfront.
//
// Returns false for pipes, /dev/null, and redirected regular files —
// all cases where the operator genuinely cannot answer prompts.
// (os.ModeCharDevice alone is insufficient: /dev/null is a character
// device too, so the check must use the TCGETS-based term.IsTerminal.)
func stdinLooksInteractive(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		// Swapped (test) reader; let the caller manage EOF.
		return true
	}
	return term.IsTerminal(int(f.Fd()))
}

// defaultNewProbeRunner returns the production runner that shells out to
// probes via the same executor `mgtt plan` builds.
func defaultNewProbeRunner(reg *providersupport.Registry) (probeRunner, error) {
	exec, err := buildExecutor(reg)
	if err != nil {
		return nil, err
	}
	return &shellProbeRunner{exec: exec, reg: reg}, nil
}

type shellProbeRunner struct {
	exec probe.Executor
	reg  *providersupport.Registry
}

func (r *shellProbeRunner) Run(ctx context.Context, p *strategy.Probe, store *facts.Store) (string, error) {
	rendered := probe.Substitute(p.Command, p.Component, p.Vars, nil)
	if err := probe.ValidateCommand(rendered, p.Command); err != nil {
		return "", err
	}
	result, err := r.exec.Run(probe.WithTracer(ctx, probe.NewTracer()), probe.Command{
		Raw:       rendered,
		Parse:     p.ParseMode,
		Provider:  p.Provider,
		Component: p.Component,
		Fact:      p.Fact,
		Type:      p.Type,
		Resource:  p.Resource,
		Vars:      p.Vars,
		Timeout:   probeTimeout(),
	})
	if err != nil {
		return handleProbeError(p, store, err)
	}
	if result.Status == probe.StatusNotFound {
		recordUnresolvedFact(store, p, facts.FactStatusNotFound, "not_found")
		return fmt.Sprintf("%s.%s = <not_found>", p.Component, p.Fact), nil
	}
	store.Append(p.Component, facts.Fact{
		Key:       p.Fact,
		Value:     result.Parsed,
		Collector: "probe",
		At:        time.Now(),
		Raw:       result.Raw,
	})
	return fmt.Sprintf("%s.%s = %v", p.Component, p.Fact, result.Parsed), nil
}

// handleProbeError degrades Forbidden/Transient failures to "unknown
// fact" (Unresolved semantics) so diagnose keeps going; all other
// taxonomies terminate the run.
func handleProbeError(p *strategy.Probe, store *facts.Store, err error) (string, error) {
	if errors.Is(err, probe.ErrForbidden) {
		recordUnresolvedFact(store, p, facts.FactStatusForbidden, "forbidden: "+err.Error())
		return fmt.Sprintf("%s.%s = <forbidden>", p.Component, p.Fact), nil
	}
	if errors.Is(err, probe.ErrTransient) {
		recordUnresolvedFact(store, p, facts.FactStatusTransient, "transient: "+err.Error())
		return fmt.Sprintf("%s.%s = <transient>", p.Component, p.Fact), nil
	}
	return "", err
}

// recordUnresolvedFact appends a value-less fact with the given status
// classification. The engine's expr layer yields an UnresolvedError on
// the next iteration so the strategy picks a different probe.
func recordUnresolvedFact(store *facts.Store, p *strategy.Probe, status facts.FactStatus, note string) {
	store.Append(p.Component, facts.Fact{
		Key:       p.Fact,
		Value:     nil,
		Collector: "probe",
		At:        time.Now(),
		Note:      note,
		Status:    status,
	})
}

// reportDone prints the terminal success report: single scenario remains,
// show the chain, trail, and suspect commentary.
func reportDone(cmd *cobra.Command, m *model.Model, root *scenarios.Scenario, store *facts.Store, trail []probeRecord, probesRun, maxProbes int, start time.Time, deadline time.Duration, suspects []strategy.SuspectHint) {
	w := cmd.OutOrStdout()
	if root == nil {
		fmt.Fprintln(w, "Root cause: (none — all components healthy)")
		writeBudget(w, probesRun, maxProbes, start, deadline)
		writePartialVisibility(w, store)
		writeTrail(w, trail)
		return
	}
	fmt.Fprintf(w, "Root cause: %s\n", formatComponentLabel(m, root.Root.Component))
	fmt.Fprintf(w, "Scenario:   %s\n", renderChain(*root))
	writeBudget(w, probesRun, maxProbes, start, deadline)
	writePartialVisibility(w, store)
	if hint := suspectReport(suspects, root); hint != "" {
		fmt.Fprintf(w, "Hint:       %s\n", hint)
	}
	writeTrail(w, trail)
}

// reportStuck prints the "observed facts contradict every enumerated
// chain" report — model-gap territory.
func reportStuck(cmd *cobra.Command, store *facts.Store, trail []probeRecord, probesRun, maxProbes int, start time.Time, deadline time.Duration) {
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "No matching scenario — observed facts contradict every enumerated chain.")
	fmt.Fprintln(w, "This likely indicates a model gap (novel failure, missing triggered_by,")
	fmt.Fprintln(w, "or new failure mode not yet declared on a type).")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Collected facts:")
	comps := store.AllComponents()
	// Stable order for deterministic output.
	sortedComps := make([]string, len(comps))
	copy(sortedComps, comps)
	sort.Strings(sortedComps)
	for _, c := range sortedComps {
		for _, f := range store.FactsFor(c) {
			fmt.Fprintf(w, "  %s.%s = %v\n", c, f.Key, f.Value)
		}
	}
	fmt.Fprintln(w)
	writeBudget(w, probesRun, maxProbes, start, deadline)
	writePartialVisibility(w, store)
	writeTrail(w, trail)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Hint: if this incident resolves, run `mgtt incident end --suggest-scenarios`")
	fmt.Fprintln(w, "to propose the missing chain for review.")
}

// reportPartial prints the "we stopped early" report. Used for budget
// exhaustion, deadline expiry, and write-probe pause.
func reportPartial(cmd *cobra.Command, store *facts.Store, trail []probeRecord, reason string, probesRun, maxProbes int, start time.Time, deadline time.Duration) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Stopped: %s\n", reason)
	writeBudget(w, probesRun, maxProbes, start, deadline)
	writePartialVisibility(w, store)
	writeTrail(w, trail)
}

func writeBudget(w io.Writer, probesRun, maxProbes int, start time.Time, deadline time.Duration) {
	elapsed := time.Since(start).Round(time.Second)
	fmt.Fprintf(w, "Probes run: %d/%d   Time: %s/%s\n", probesRun, maxProbes, elapsed, deadline)
}

// writePartialVisibility surfaces how many facts the probe layer could
// not resolve due to a recoverable error (forbidden / transient) rather
// than a value. Those facts are degraded to unresolved and the engine
// continues, but the operator must know the conclusion was drawn under
// partial visibility — the real root cause may sit behind one of the
// forbidden probes. The count reads the authoritative Fact.Status from
// the store (not the rendered outcome strings) so it stays correct
// regardless of how an outcome was phrased or which path recorded it.
func writePartialVisibility(w io.Writer, store *facts.Store) {
	forbidden, transient := store.PartialVisibility()
	if forbidden == 0 && transient == 0 {
		return
	}
	parts := make([]string, 0, 2)
	if forbidden > 0 {
		parts = append(parts, fmt.Sprintf("%d forbidden (RBAC / IAM refused)", forbidden))
	}
	if transient > 0 {
		parts = append(parts, fmt.Sprintf("%d transient (throttled / timed out)", transient))
	}
	fmt.Fprintf(w, "Partial visibility: %s — result may be incomplete.\n", strings.Join(parts, ", "))
}

func writeTrail(w io.Writer, trail []probeRecord) {
	if len(trail) == 0 {
		return
	}
	fmt.Fprintln(w, "Trail:")
	// Only annotate the resource on the FIRST row per component — spec
	// §5.2 says "a subtitle on the first mention of each component".
	// Repeating it on every row adds noise with zero new information.
	seen := make(map[string]bool, len(trail))
	for i, r := range trail {
		label := formatProbeLabel(r.probe, seen)
		if r.probe != nil {
			seen[r.probe.Component] = true
		}
		fmt.Fprintf(w, "  %d. %s — %s\n", i+1, label, r.outcome)
	}
}

// formatProbeLabel returns a human-readable "component.fact" label.
// When the probe targets a distinct upstream resource AND this is the
// first mention of the component in the trail (seen map), the label
// gains a "(resource: <id>)" annotation. Subsequent probes on the same
// component skip the suffix to keep the trail compact.
func formatProbeLabel(p *strategy.Probe, seen map[string]bool) string {
	if p == nil {
		return ""
	}
	base := fmt.Sprintf("%s.%s", p.Component, p.Fact)
	if p.Resource == "" || p.Resource == p.Component {
		return base
	}
	if seen != nil && seen[p.Component] {
		return base
	}
	return fmt.Sprintf("%s (resource: %s)", base, p.Resource)
}

// formatComponentLabel returns the component name, annotated with
// "(resource: <id>)" when the model declares an explicit Resource
// override that differs from the component key. Used in the Done
// report so the operator sees the upstream identifier without
// cross-referencing the model file.
func formatComponentLabel(m *model.Model, name string) string {
	if m == nil {
		return name
	}
	comp := m.Components[name]
	if comp == nil {
		return name
	}
	if comp.Resource != "" && comp.Resource != name {
		return fmt.Sprintf("%s (resource: %s)", name, comp.Resource)
	}
	return name
}

// renderChain returns "rds.stopped → api.crash_looping → nginx.degraded".
// Reads chain in declaration order (root → terminal).
func renderChain(s scenarios.Scenario) string {
	parts := make([]string, 0, len(s.Chain))
	for _, step := range s.Chain {
		parts = append(parts, fmt.Sprintf("%s.%s", step.Component, step.State))
	}
	return strings.Join(parts, " → ")
}

// suspectReport compares each operator-supplied suspect against the
// winning scenario. Three outcomes:
//   - confirmed: the suspect sits at the scenario's root.
//   - appeared mid-chain: suspect was downstream; real root was elsewhere.
//   - ignored: suspect never appeared in the chain at all.
func suspectReport(suspects []strategy.SuspectHint, root *scenarios.Scenario) string {
	if root == nil || len(suspects) == 0 {
		return ""
	}
	var parts []string
	for _, h := range suspects {
		if h.Component == root.Root.Component {
			parts = append(parts, fmt.Sprintf("suspect=%s — confirmed as root", h.Component))
			continue
		}
		if root.TouchesComponent(h.Component) {
			parts = append(parts, fmt.Sprintf("suspect=%s — appeared mid-chain; real root was %s", h.Component, root.Root.Component))
			continue
		}
		parts = append(parts, fmt.Sprintf("suspect=%s — ignored (not on root chain)", h.Component))
	}
	return strings.Join(parts, "; ")
}
