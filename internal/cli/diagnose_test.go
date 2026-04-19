package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"

	"github.com/spf13/cobra"
)

// stubProbeRunner serves canned fact values for (component, fact) pairs.
// Any probe not in values returns an error so tests fail loud on unexpected
// probe paths.
type stubProbeRunner struct {
	values map[string]any // "comp.fact" → value
	delays map[string]time.Duration
	calls  []string // ordered list of "comp.fact" actually probed
}

func (s *stubProbeRunner) Run(ctx context.Context, p *strategy.Probe, store *facts.Store) (string, error) {
	key := p.Component + "." + p.Fact
	s.calls = append(s.calls, key)
	if d, ok := s.delays[key]; ok {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	val, ok := s.values[key]
	if !ok {
		return "", fmt.Errorf("stub: no canned value for %s", key)
	}
	store.Append(p.Component, facts.Fact{
		Key:   p.Fact,
		Value: val,
		At:    time.Now(),
	})
	return fmt.Sprintf("%s = %v", key, val), nil
}

// diagnoseFixture constructs a two-component model (api → db) with a
// status fact per component, plus the matching registry.
func diagnoseFixture(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()
	mkType := func(name string) *providersupport.Type {
		return &providersupport.Type{
			Name: name,
			Facts: map[string]*providersupport.FactSpec{
				"status": {Probe: providersupport.ProbeDef{Cmd: name + "-status", Cost: "cheap", Access: "read"}},
			},
			States: []providersupport.StateDef{
				{Name: "down", When: expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "down"}},
			},
		}
	}
	prov := &providersupport.Provider{
		Meta:     providersupport.ProviderMeta{Name: "p"},
		Types:    map[string]*providersupport.Type{"api": mkType("api"), "db": mkType("db")},
		ReadOnly: true,
	}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"api": {Name: "api", Type: "api", Depends: []model.Dependency{{On: []string{"db"}}}},
			"db":  {Name: "db", Type: "db"},
		},
		Order: []string{"api", "db"},
	}
	m.BuildGraph()
	return m, reg
}

// withLoader installs a one-shot loader for the duration of the test.
func withLoader(t *testing.T, m *model.Model, reg *providersupport.Registry, scs []scenarios.Scenario) {
	t.Helper()
	prev := diagnoseLoader
	diagnoseLoader = func(_ string) (*model.Model, *providersupport.Registry, []scenarios.Scenario, error) {
		return m, reg, scs, nil
	}
	t.Cleanup(func() { diagnoseLoader = prev })
}

// withRunner installs a one-shot probe runner for the duration of the test.
func withRunner(t *testing.T, r probeRunner) {
	t.Helper()
	prev := newProbeRunner
	newProbeRunner = func(_ *providersupport.Registry) (probeRunner, error) {
		return r, nil
	}
	t.Cleanup(func() { newProbeRunner = prev })
}

// withStdin redirects diagnose's prompt reader for the duration of the test.
func withStdin(t *testing.T, r io.Reader) {
	t.Helper()
	prev := diagnoseStdin
	diagnoseStdin = r
	t.Cleanup(func() { diagnoseStdin = prev })
}

// runDiagnoseCaptured invokes runDiagnose against a buffer-backed cobra
// command so tests can inspect the emitted output without touching
// rootCmd state.
func runDiagnoseCaptured(t *testing.T, f diagnoseFlags) (string, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "diagnose"}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())
	err := runDiagnose(cmd, f)
	return buf.String(), err
}

func TestDiagnose_Done(t *testing.T) {
	m, reg := diagnoseFixtureMultiState(t)
	// Two scenarios, both terminal at api but with different root states.
	// The first probe on api.status collapses the live set by confirming
	// "down" (eliminating the degraded-rooted scenario).
	scs := []scenarios.Scenario{
		{
			ID:   "api-down",
			Root: scenarios.RootRef{Component: "api", State: "down"},
			Chain: []scenarios.Step{
				{Component: "api", State: "down", Observes: []string{"status"}},
			},
		},
		{
			ID:   "api-degraded",
			Root: scenarios.RootRef{Component: "api", State: "degraded"},
			Chain: []scenarios.Step{
				{Component: "api", State: "degraded", Observes: []string{"status"}},
			},
		},
	}
	withLoader(t, m, reg, scs)
	withRunner(t, &stubProbeRunner{values: map[string]any{
		"api.status": "down", // confirms api.down; contradicts api.degraded
	}})

	out, err := runDiagnoseCaptured(t, diagnoseFlags{maxProbes: 10, deadline: 5 * time.Second, onWrite: "pause", readonlyOnly: true})
	if err != nil {
		t.Fatalf("runDiagnose: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "Root cause: api") {
		t.Errorf("want 'Root cause: api' in output; got:\n%s", out)
	}
	if !strings.Contains(out, "Scenario:") {
		t.Errorf("want scenario line; got:\n%s", out)
	}
}

// diagnoseFixtureMultiState is a variant where "api" has two distinct
// non-default states (down, degraded) so tests can force a live-set
// collapse on a single probe.
func diagnoseFixtureMultiState(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()
	apiType := &providersupport.Type{
		Name: "api",
		Facts: map[string]*providersupport.FactSpec{
			"status": {Probe: providersupport.ProbeDef{Cmd: "api-status", Cost: "cheap", Access: "read"}},
		},
		States: []providersupport.StateDef{
			{Name: "down", When: expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "down"}},
			{Name: "degraded", When: expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "degraded"}},
		},
	}
	prov := &providersupport.Provider{
		Meta:     providersupport.ProviderMeta{Name: "p"},
		Types:    map[string]*providersupport.Type{"api": apiType},
		ReadOnly: true,
	}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"api": {Name: "api", Type: "api"},
		},
		Order: []string{"api"},
	}
	m.BuildGraph()
	return m, reg
}

func TestDiagnose_Stuck(t *testing.T) {
	m, reg := diagnoseFixtureMultiState(t)
	// Two scenarios so occam actually suggests probes; the single probe
	// returns "up" which contradicts both down and degraded → live set
	// collapses to zero.
	scs := []scenarios.Scenario{
		{
			ID:   "api-down",
			Root: scenarios.RootRef{Component: "api", State: "down"},
			Chain: []scenarios.Step{
				{Component: "api", State: "down", Observes: []string{"status"}},
			},
		},
		{
			ID:   "api-degraded",
			Root: scenarios.RootRef{Component: "api", State: "degraded"},
			Chain: []scenarios.Step{
				{Component: "api", State: "degraded", Observes: []string{"status"}},
			},
		},
	}
	withLoader(t, m, reg, scs)
	withRunner(t, &stubProbeRunner{values: map[string]any{
		"api.status": "up", // contradicts both "down" and "degraded"
	}})

	out, err := runDiagnoseCaptured(t, diagnoseFlags{maxProbes: 10, deadline: 5 * time.Second, onWrite: "pause", readonlyOnly: true})
	if err != nil {
		t.Fatalf("runDiagnose: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "No matching scenario") {
		t.Errorf("want 'No matching scenario' in output; got:\n%s", out)
	}
	if !strings.Contains(out, "model gap") {
		t.Errorf("want 'model gap' in output; got:\n%s", out)
	}
}

func TestDiagnose_BudgetExhausted(t *testing.T) {
	m, reg := diagnoseFixture(t)
	// Three scenarios that all stay live under the facts we'll feed.
	// The loop should keep probing until max-probes hits.
	scs := []scenarios.Scenario{
		{ID: "s1", Root: scenarios.RootRef{Component: "api", State: "down"}, Chain: []scenarios.Step{{Component: "api", State: "down", Observes: []string{"status"}}}},
		{ID: "s2", Root: scenarios.RootRef{Component: "db", State: "down"}, Chain: []scenarios.Step{{Component: "db", State: "down"}, {Component: "api", State: "down", Observes: []string{"status"}}}},
		{ID: "s3", Root: scenarios.RootRef{Component: "db", State: "down"}, Chain: []scenarios.Step{{Component: "db", State: "down", Observes: []string{"status"}}}},
	}
	withLoader(t, m, reg, scs)
	// Both probes return "down" so nothing gets eliminated.
	withRunner(t, &stubProbeRunner{values: map[string]any{
		"api.status": "down",
		"db.status":  "down",
	}})

	out, err := runDiagnoseCaptured(t, diagnoseFlags{maxProbes: 2, deadline: 5 * time.Second, onWrite: "pause", readonlyOnly: true})
	if err != nil {
		t.Fatalf("runDiagnose: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "budget exhausted") {
		t.Errorf("want 'budget exhausted' in output; got:\n%s", out)
	}
}

func TestDiagnose_DeadlineExceeded(t *testing.T) {
	m, reg := diagnoseFixture(t)
	scs := []scenarios.Scenario{
		{ID: "s1", Root: scenarios.RootRef{Component: "api", State: "down"}, Chain: []scenarios.Step{{Component: "api", State: "down", Observes: []string{"status"}}}},
		{ID: "s2", Root: scenarios.RootRef{Component: "db", State: "down"}, Chain: []scenarios.Step{{Component: "db", State: "down"}, {Component: "api", State: "down", Observes: []string{"status"}}}},
	}
	withLoader(t, m, reg, scs)
	// Make the first probe sleep past the deadline.
	withRunner(t, &stubProbeRunner{
		values: map[string]any{"api.status": "down", "db.status": "down"},
		delays: map[string]time.Duration{"api.status": 200 * time.Millisecond},
	})

	out, err := runDiagnoseCaptured(t, diagnoseFlags{maxProbes: 10, deadline: 50 * time.Millisecond, onWrite: "pause", readonlyOnly: true})
	if err != nil {
		t.Fatalf("runDiagnose: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "deadline exceeded") {
		t.Errorf("want 'deadline exceeded' in output; got:\n%s", out)
	}
}

func TestDiagnose_WritePauseTriggered(t *testing.T) {
	// Custom fixture where the provider is NOT read-only.
	writeProv := &providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "wp"},
		Types: map[string]*providersupport.Type{
			"api": {
				Name: "api",
				Facts: map[string]*providersupport.FactSpec{
					"status": {Probe: providersupport.ProbeDef{Cmd: "api-status", Cost: "high", Access: "write"}},
				},
				States: []providersupport.StateDef{
					{Name: "down", When: expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "down"}},
				},
			},
		},
		ReadOnly: false,
	}
	reg := providersupport.NewRegistry()
	reg.Register(writeProv)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"wp"}},
		Components: map[string]*model.Component{
			"api": {Name: "api", Type: "api"},
		},
		Order: []string{"api"},
	}
	m.BuildGraph()

	scs := []scenarios.Scenario{
		// Two scenarios keep the set live so occam actually suggests a probe.
		{ID: "s1", Root: scenarios.RootRef{Component: "api", State: "down"}, Chain: []scenarios.Step{{Component: "api", State: "down", Observes: []string{"status"}}}},
		{ID: "s2", Root: scenarios.RootRef{Component: "api", State: "down"}, Chain: []scenarios.Step{{Component: "api", State: "down", Observes: []string{"status"}}}},
	}
	withLoader(t, m, reg, scs)
	// Runner shouldn't be called — pause gates the write probe first. If
	// it is called we surface a loud error.
	withRunner(t, &stubProbeRunner{values: map[string]any{}})

	out, err := runDiagnoseCaptured(t, diagnoseFlags{maxProbes: 10, deadline: 5 * time.Second, onWrite: "pause", readonlyOnly: true})
	if err != nil {
		t.Fatalf("runDiagnose: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "pause") {
		t.Errorf("want 'pause' in output; got:\n%s", out)
	}
	if !strings.Contains(out, "requires writes") {
		t.Errorf("want 'requires writes' in output; got:\n%s", out)
	}
}

func TestDiagnose_GenericComponentPrompt(t *testing.T) {
	// Fixture with two components: a "widget" backed by the generic
	// provider, and a "db" backed by a normal provider. Two scenarios
	// force occam to suggest a probe — the widget-targeted one triggers
	// the operator prompt.
	genericProv := &providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "generic"},
		Types: map[string]*providersupport.Type{
			"thing": {
				Name: "thing",
				Facts: map[string]*providersupport.FactSpec{
					"operator_says_healthy": {Probe: providersupport.ProbeDef{Cmd: "", Cost: "free", Access: "none"}},
				},
				States: []providersupport.StateDef{
					{Name: "down", When: expr.CmpNode{Fact: "operator_says_healthy", Op: expr.OpEq, Value: false}},
				},
			},
		},
		ReadOnly: true,
	}
	normalProv := &providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "np"},
		Types: map[string]*providersupport.Type{
			"db": {
				Name: "db",
				Facts: map[string]*providersupport.FactSpec{
					"status": {Probe: providersupport.ProbeDef{Cmd: "db-status", Cost: "cheap", Access: "read"}},
				},
				States: []providersupport.StateDef{
					{Name: "down", When: expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "down"}},
				},
			},
		},
		ReadOnly: true,
	}
	reg := providersupport.NewRegistry()
	reg.Register(genericProv)
	reg.Register(normalProv)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"generic", "np"}},
		Components: map[string]*model.Component{
			"widget": {Name: "widget", Type: "thing"},
			"db":     {Name: "db", Type: "db"},
		},
		Order: []string{"widget", "db"},
	}
	m.BuildGraph()

	scs := []scenarios.Scenario{
		{ID: "s1", Root: scenarios.RootRef{Component: "widget", State: "down"}, Chain: []scenarios.Step{{Component: "widget", State: "down", Observes: []string{"operator_says_healthy"}}}},
		{ID: "s2", Root: scenarios.RootRef{Component: "db", State: "down"}, Chain: []scenarios.Step{{Component: "db", State: "down", Observes: []string{"status"}}}},
	}
	withLoader(t, m, reg, scs)
	// Runner might be called for the db scenario; return "down" so db.down
	// stays live and the loop eventually budgets out after the generic
	// prompt fires.
	withRunner(t, &stubProbeRunner{values: map[string]any{"db.status": "down"}})

	// Feed multiple "n" answers in case the prompt fires more than once.
	withStdin(t, strings.NewReader("n\nn\nn\n"))

	out, err := runDiagnoseCaptured(t, diagnoseFlags{maxProbes: 3, deadline: 5 * time.Second, onWrite: "pause", readonlyOnly: true})
	if err != nil {
		t.Fatalf("runDiagnose: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "Is 'widget' healthy?") {
		t.Errorf("want prompt in output; got:\n%s", out)
	}
	// The operator-answer trail line must appear in the final report.
	if !strings.Contains(out, "operator-answered: n") {
		t.Errorf("want 'operator-answered: n' in trail; got:\n%s", out)
	}
}

// TestDiagnose_EOFStdinRecordsSkipMarker verifies that when stdin
// delivers EOF before any answer, the generic-component prompt loop
// bails out with a partial report instead of spinning on empty reads
// until --max-probes is exhausted. The skip-marker fact is recorded so
// pickSymptomInward doesn't re-select the same step in the same loop
// iteration set.
func TestDiagnose_EOFStdinRecordsSkipMarker(t *testing.T) {
	genericProv := &providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "generic"},
		Types: map[string]*providersupport.Type{
			"thing": {
				Name: "thing",
				Facts: map[string]*providersupport.FactSpec{
					"operator_says_healthy": {Probe: providersupport.ProbeDef{Cmd: "", Cost: "free", Access: "none"}},
				},
				States: []providersupport.StateDef{
					{Name: "down", When: expr.CmpNode{Fact: "operator_says_healthy", Op: expr.OpEq, Value: false}},
				},
			},
		},
		ReadOnly: true,
	}
	reg := providersupport.NewRegistry()
	reg.Register(genericProv)
	m := &model.Model{
		Meta: model.Meta{Providers: []string{"generic"}},
		Components: map[string]*model.Component{
			"widget": {Name: "widget", Type: "thing"},
		},
		Order: []string{"widget"},
	}
	m.BuildGraph()

	scs := []scenarios.Scenario{
		{ID: "s1", Root: scenarios.RootRef{Component: "widget", State: "down"}, Chain: []scenarios.Step{{Component: "widget", State: "down", Observes: []string{"operator_says_healthy"}}}},
		{ID: "s2", Root: scenarios.RootRef{Component: "widget", State: "down"}, Chain: []scenarios.Step{{Component: "widget", State: "down", Observes: []string{"operator_says_healthy"}}}},
	}
	withLoader(t, m, reg, scs)
	withRunner(t, &stubProbeRunner{values: map[string]any{}})
	// Empty reader → immediate EOF.
	withStdin(t, strings.NewReader(""))

	out, err := runDiagnoseCaptured(t, diagnoseFlags{maxProbes: 20, deadline: 5 * time.Second, onWrite: "pause", readonlyOnly: true})
	if err != nil {
		t.Fatalf("EOF must not error; got %v\nout:\n%s", err, out)
	}
	if !strings.Contains(out, "stdin closed") {
		t.Errorf("want 'stdin closed' in output; got:\n%s", out)
	}
	if !strings.Contains(out, "Stopped:") {
		t.Errorf("want 'Stopped:' in output; got:\n%s", out)
	}
}

// TestDiagnose_NonTTYStdinRejectedForGeneric verifies diagnose fails fast
// when the model has generic components AND stdin is a real non-TTY
// file. Using /dev/null here simulates the redirect-from-file case.
func TestDiagnose_NonTTYStdinRejectedForGeneric(t *testing.T) {
	genericProv := &providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "generic"},
		Types: map[string]*providersupport.Type{
			"thing": {
				Name: "thing",
				Facts: map[string]*providersupport.FactSpec{
					"operator_says_healthy": {Probe: providersupport.ProbeDef{Cmd: "", Cost: "free", Access: "none"}},
				},
				States: []providersupport.StateDef{
					{Name: "down", When: expr.CmpNode{Fact: "operator_says_healthy", Op: expr.OpEq, Value: false}},
				},
			},
		},
		ReadOnly: true,
	}
	reg := providersupport.NewRegistry()
	reg.Register(genericProv)
	m := &model.Model{
		Meta: model.Meta{Providers: []string{"generic"}},
		Components: map[string]*model.Component{
			"widget": {Name: "widget", Type: "thing"},
		},
		Order: []string{"widget"},
	}
	m.BuildGraph()
	scs := []scenarios.Scenario{
		{ID: "s1", Root: scenarios.RootRef{Component: "widget", State: "down"}, Chain: []scenarios.Step{{Component: "widget", State: "down", Observes: []string{"operator_says_healthy"}}}},
	}
	withLoader(t, m, reg, scs)
	withRunner(t, &stubProbeRunner{values: map[string]any{}})

	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("no %s on this platform: %v", os.DevNull, err)
	}
	t.Cleanup(func() { devnull.Close() })
	withStdin(t, devnull)

	out, err := runDiagnoseCaptured(t, diagnoseFlags{maxProbes: 20, deadline: 5 * time.Second, onWrite: "pause", readonlyOnly: true})
	if err == nil {
		t.Fatalf("non-TTY stdin + generic must error fast; got nil\nout:\n%s", out)
	}
	if !strings.Contains(err.Error(), "interactive terminal") {
		t.Errorf("error should explain the TTY requirement; got %v", err)
	}
}

// TestDiagnose_ParseSuspectHints spot-checks the hint parser.
func TestDiagnose_ParseSuspectHints(t *testing.T) {
	got := parseSuspectHints([]string{"api", "db.down", "", "  "})
	if len(got) != 2 {
		t.Fatalf("want 2 hints; got %d (%+v)", len(got), got)
	}
	if got[0] != (strategy.SuspectHint{Component: "api"}) {
		t.Errorf("hint[0]: want {api,\"\"}; got %+v", got[0])
	}
	if got[1] != (strategy.SuspectHint{Component: "db", State: "down"}) {
		t.Errorf("hint[1]: want {db,down}; got %+v", got[1])
	}
}
