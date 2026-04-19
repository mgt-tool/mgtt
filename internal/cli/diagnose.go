package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// diagnoseLoaderFn returns the model, registry, and scenarios the
// diagnose loop will operate on. Tests replace this to inject synthetic
// fixtures without touching disk.
type diagnoseLoaderFn func(modelPathHint string) (*model.Model, *providersupport.Registry, []scenarios.Scenario, error)

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
	cmd.Flags().StringSliceVar(&f.suspect, "suspect", nil, "comma-separated components (or component.state tuples) that seem broken — soft prior, not a filter; splits on the first '.' so component names containing a dot will mis-parse")
	cmd.Flags().BoolVar(&f.readonlyOnly, "readonly-only", true, "only run probes whose provider declares read_only: true")
	cmd.Flags().IntVar(&f.maxProbes, "max-probes", 20, "probe budget")
	cmd.Flags().DurationVar(&f.deadline, "deadline", 5*time.Minute, "wall-clock deadline")
	cmd.Flags().StringVar(&f.onWrite, "on-write", "pause", "behavior when a write-probe is next: pause|run|fail")
	return cmd
}

func init() {
	rootCmd.AddCommand(newDiagnoseCmd())
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
	var store *facts.Store
	if inc, incErr := incident.Current(); incErr == nil && inc.Store != nil {
		store = inc.Store
	} else {
		store = facts.NewInMemory()
	}
	suspects := parseSuspectHints(f.suspect)
	trail := []probeRecord{}
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

	for probesRun < f.maxProbes {
		if ctx.Err() != nil {
			reportPartial(cmd, trail, "deadline exceeded", probesRun, f.maxProbes, start, f.deadline)
			return nil
		}
		input := strategy.Input{Model: m, Registry: reg, Store: store, Scenarios: scs, Suspects: suspects}
		strat := strategy.AutoSelect(input)
		decision := strat.SuggestProbe(input)

		switch {
		case decision.Done:
			reportDone(cmd, m, decision.RootCause, trail, probesRun, f.maxProbes, start, f.deadline, suspects)
			return nil
		case decision.Stuck:
			reportStuck(cmd, store, trail, probesRun, f.maxProbes, start, f.deadline)
			return nil
		case decision.Probe == nil:
			reportPartial(cmd, trail, "strategy returned no probe", probesRun, f.maxProbes, start, f.deadline)
			return nil
		}

		p := decision.Probe

		// Generic-component gate: prompt operator instead of shelling out.
		if isGenericComponent(m, reg, p.Component) {
			answer, err := promptYesNoSkip(cmd, p.Component)
			if err != nil {
				if err == errNoMoreAnswers {
					// Stdin closed mid-session; don't re-prompt forever.
					// Record a sentinel so Occam sees the step as
					// "verified-but-skipped" and won't re-select it.
					applyOperatorAnswer(store, p.Component, "skip")
					trail = append(trail, probeRecord{probe: p, outcome: "operator-answered: skip (stdin closed)"})
					reportPartial(cmd, trail, "no more operator input (stdin closed)", probesRun+1, f.maxProbes, start, f.deadline)
					return nil
				}
				return err
			}
			applyOperatorAnswer(store, p.Component, answer)
			trail = append(trail, probeRecord{probe: p, outcome: fmt.Sprintf("operator-answered: %s", answer)})
			probesRun++
			continue
		}

		// Read-only gate — only enforce when the operator asked for it.
		if f.readonlyOnly && !probeIsReadOnly(p, m, reg) {
			switch f.onWrite {
			case "pause":
				reportPartial(cmd, trail, fmt.Sprintf("next probe requires writes (component=%s fact=%s); --on-write=pause", p.Component, p.Fact), probesRun, f.maxProbes, start, f.deadline)
				return nil
			case "fail":
				return fmt.Errorf("write probe encountered: %s.%s (--on-write=fail)", p.Component, p.Fact)
			case "run":
				// fall through
			default:
				return fmt.Errorf("invalid --on-write value %q", f.onWrite)
			}
		}

		outcome, err := runner.Run(ctx, p, store)
		if err != nil {
			if ctx.Err() != nil {
				reportPartial(cmd, trail, "deadline exceeded", probesRun, f.maxProbes, start, f.deadline)
				return nil
			}
			return fmt.Errorf("probe %s.%s: %w", p.Component, p.Fact, err)
		}
		trail = append(trail, probeRecord{probe: p, outcome: outcome})
		probesRun++
	}

	reportPartial(cmd, trail, "budget exhausted", probesRun, f.maxProbes, start, f.deadline)
	return nil
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

	var scs []scenarios.Scenario
	scenariosPath := filepath.Join(filepath.Dir(path), "scenarios.yaml")
	if _, err := os.Stat(scenariosPath); err == nil {
		f, err := os.Open(scenariosPath)
		if err != nil {
			return nil, nil, nil, err
		}
		loaded, _, err := scenarios.Read(f)
		f.Close()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read %s: %w", scenariosPath, err)
		}
		scs = loaded
	}
	return m, reg, scs, nil
}

// parseSuspectHints turns raw --suspect entries into structured hints.
// "api" is a component-only hint (any state); "api.crash_looping" pins
// the state too. Empty strings are skipped.
func parseSuspectHints(raw []string) []strategy.SuspectHint {
	var out []strategy.SuspectHint
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if dot := strings.IndexByte(entry, '.'); dot >= 0 {
			out = append(out, strategy.SuspectHint{Component: entry[:dot], State: entry[dot+1:]})
			continue
		}
		out = append(out, strategy.SuspectHint{Component: entry})
	}
	return out
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
	providers := comp.Providers
	if len(providers) == 0 {
		providers = m.Meta.Providers
	}
	_, providerName, err := reg.ResolveType(providers, comp.Type)
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

// modelUsesGenericComponent returns true if any component in the model
// resolves to the built-in generic provider.
func modelUsesGenericComponent(m *model.Model, reg *providersupport.Registry) bool {
	if m == nil || reg == nil {
		return false
	}
	for name := range m.Components {
		if isGenericComponent(m, reg, name) {
			return true
		}
	}
	return false
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
	tracedCtx := probe.WithTracer(ctx, probe.NewTracer())
	result, err := r.exec.Run(tracedCtx, probe.Command{
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
		return "", err
	}
	if result.Status == probe.StatusNotFound {
		store.Append(p.Component, facts.Fact{
			Key:       p.Fact,
			Value:     nil,
			Collector: "probe",
			At:        time.Now(),
			Note:      "not_found",
			Status:    facts.FactStatusNotFound,
		})
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

// reportDone prints the terminal success report: single scenario remains,
// show the chain, trail, and suspect commentary.
func reportDone(cmd *cobra.Command, m *model.Model, root *scenarios.Scenario, trail []probeRecord, probesRun, maxProbes int, start time.Time, deadline time.Duration, suspects []strategy.SuspectHint) {
	w := cmd.OutOrStdout()
	if root == nil {
		fmt.Fprintln(w, "Root cause: (none — all components healthy)")
		writeBudget(w, probesRun, maxProbes, start, deadline)
		writeTrail(w, trail)
		return
	}
	fmt.Fprintf(w, "Root cause: %s\n", formatComponentLabel(m, root.Root.Component))
	fmt.Fprintf(w, "Scenario:   %s\n", renderChain(*root))
	writeBudget(w, probesRun, maxProbes, start, deadline)
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
	writeTrail(w, trail)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Hint: if this incident resolves, run `mgtt incident end --suggest-scenarios`")
	fmt.Fprintln(w, "to propose the missing chain for review.")
}

// reportPartial prints the "we stopped early" report. Used for budget
// exhaustion, deadline expiry, and write-probe pause.
func reportPartial(cmd *cobra.Command, trail []probeRecord, reason string, probesRun, maxProbes int, start time.Time, deadline time.Duration) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Stopped: %s\n", reason)
	writeBudget(w, probesRun, maxProbes, start, deadline)
	writeTrail(w, trail)
}

func writeBudget(w io.Writer, probesRun, maxProbes int, start time.Time, deadline time.Duration) {
	elapsed := time.Since(start).Round(time.Second)
	fmt.Fprintf(w, "Probes run: %d/%d   Time: %s/%s\n", probesRun, maxProbes, elapsed, deadline)
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
