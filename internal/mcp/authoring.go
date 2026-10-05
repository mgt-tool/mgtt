// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"fmt"
	"os"
	"sort"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/modeldiff"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
	"github.com/mgt-tool/mgtt/internal/simulate"
)

// The authoring tools help a client write a model: read the installed
// vocabulary, check a draft. They read installed providers and the model
// they are given, never a live system, and never write anything.

// maxModelSourceBytes caps an inline model_source. A model is a few
// hundred lines; anything near this is a mistake, not a model.
const maxModelSourceBytes = 512 << 10

// TypesListParams optionally narrows types_list to one provider.
type TypesListParams struct {
	Provider string `json:"provider,omitempty"`
}

// TypeSummary is one type in the installed vocabulary.
type TypeSummary struct {
	Provider    string `json:"provider"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Facts       int    `json:"facts"`
}

// TypesListResult lists types, sorted by provider then type.
type TypesListResult struct {
	Types []TypeSummary `json:"types"`
}

// TypesList returns every type the installed providers define.
func (h *Handler) TypesList(p TypesListParams) (*TypesListResult, error) {
	reg, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	out := &TypesListResult{Types: []TypeSummary{}}
	for _, prov := range reg.All() {
		if p.Provider != "" && prov.Meta.Name != p.Provider {
			continue
		}
		for name, t := range prov.Types {
			out.Types = append(out.Types, TypeSummary{Provider: prov.Meta.Name, Type: name, Description: t.Description, Facts: len(t.Facts)})
		}
	}
	if p.Provider != "" && len(out.Types) == 0 {
		return nil, fmt.Errorf("no installed provider %q defines types (types_list without a provider lists them all)", p.Provider)
	}
	sort.Slice(out.Types, func(i, j int) bool {
		if out.Types[i].Provider != out.Types[j].Provider {
			return out.Types[i].Provider < out.Types[j].Provider
		}
		return out.Types[i].Type < out.Types[j].Type
	})
	return out, nil
}

// TypesDescribeParams names a type, optionally qualified by provider.
type TypesDescribeParams struct {
	Type     string `json:"type"`
	Provider string `json:"provider,omitempty"`
}

// FactInfo is one fact a type exposes.
type FactInfo struct {
	Name   string `json:"name"`
	Type   string `json:"type,omitempty"` // mgtt.int, mgtt.bool, ...
	Cost   string `json:"cost,omitempty"`
	Access string `json:"access,omitempty"`
}

// StateInfo is one state a type can be in.
type StateInfo struct {
	Name        string   `json:"name"`
	When        string   `json:"when,omitempty"`
	Description string   `json:"description,omitempty"`
	TriggeredBy []string `json:"triggered_by,omitempty"`
	CanCause    []string `json:"can_cause,omitempty"`
}

// VariableInfo is a variable the type's provider declares: what a
// component may set under vars:, e.g. a threshold its rules compare with.
type VariableInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
}

// TypesDescribeResult is everything a model author needs about a type
// before using or overriding it.
type TypesDescribeResult struct {
	Provider           string         `json:"provider"`
	Type               string         `json:"type"`
	Description        string         `json:"description,omitempty"`
	Facts              []FactInfo     `json:"facts"`
	Healthy            []string       `json:"healthy"`
	States             []StateInfo    `json:"states"`
	DefaultActiveState string         `json:"default_active_state,omitempty"`
	Variables          []VariableInfo `json:"variables,omitempty"`
}

// TypesDescribe returns a type's facts, healthy rules, states and failure
// modes. A `healthy:` override replaces the rules listed here, so read
// them before writing one.
func (h *Handler) TypesDescribe(p TypesDescribeParams) (*TypesDescribeResult, error) {
	if p.Type == "" {
		return nil, fmt.Errorf("type is required")
	}
	reg, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	var scope []string
	if p.Provider != "" {
		scope = []string{p.Provider}
	} else {
		for _, prov := range reg.All() {
			if prov.Meta.Name != providersupport.GenericProviderName {
				scope = append(scope, prov.Meta.Name)
			}
		}
	}
	t, provName, err := reg.ResolveType(scope, p.Type)
	// Resolution falls back to the generic component for a type nobody
	// defines; describing that would describe a type that does not exist.
	if err == nil && provName == providersupport.GenericProviderName && p.Provider != providersupport.GenericProviderName {
		err = fmt.Errorf("no installed provider defines it")
	}
	if err != nil {
		return nil, fmt.Errorf("type %q: %w (types_list shows what is installed)", p.Type, err)
	}
	prov, _ := reg.Get(provName)
	out := &TypesDescribeResult{
		Provider:           provName,
		Type:               t.Name,
		Description:        t.Description,
		Facts:              []FactInfo{},
		Healthy:            append([]string{}, t.HealthyRaw...),
		States:             []StateInfo{},
		DefaultActiveState: t.DefaultActiveState,
	}
	for name, f := range t.Facts {
		out.Facts = append(out.Facts, FactInfo{Name: name, Type: f.TypeName, Cost: f.Probe.Cost, Access: f.Probe.Access})
	}
	sort.Slice(out.Facts, func(i, j int) bool { return out.Facts[i].Name < out.Facts[j].Name })
	for _, st := range t.States {
		out.States = append(out.States, StateInfo{Name: st.Name, When: st.WhenRaw, Description: st.Description, TriggeredBy: st.TriggeredBy, CanCause: t.FailureModes[st.Name]})
	}
	if prov != nil {
		for name, v := range prov.Variables {
			out.Variables = append(out.Variables, VariableInfo{Name: name, Description: v.Description, Required: v.Required, Default: v.Default})
		}
		sort.Slice(out.Variables, func(i, j int) bool { return out.Variables[i].Name < out.Variables[j].Name })
	}
	return out, nil
}

// ModelValidateParams takes the model as a path the server can read or as
// inline YAML: exactly one.
type ModelValidateParams struct {
	ModelPath   string `json:"model_path,omitempty"`
	ModelSource string `json:"model_source,omitempty"`
}

// Finding is one validation error or warning.
type Finding struct {
	Component  string `json:"component,omitempty"`
	Field      string `json:"field"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// ModelValidateResult reports every finding at once, so a client fixes a
// draft in one round rather than one error per call.
type ModelValidateResult struct {
	OK         bool      `json:"ok"`
	Components int       `json:"components"`
	Errors     []Finding `json:"errors"`
	Warnings   []Finding `json:"warnings"`
}

// ModelValidate checks a model against the installed vocabulary: the same
// checks as `mgtt model validate`, as data. Warnings include a healthy:
// override that drops a type rule, a rule reading a variable nothing sets,
// and a type no installed provider defines.
func (h *Handler) ModelValidate(p ModelValidateParams) (*ModelValidateResult, error) {
	m, err := loadModelParam(p.ModelPath, p.ModelSource)
	if err != nil {
		return nil, err
	}
	reg, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	res := model.Validate(m, reg)
	out := &ModelValidateResult{OK: !res.HasErrors(), Components: len(m.Components), Errors: []Finding{}, Warnings: []Finding{}}
	for _, e := range res.Errors {
		out.Errors = append(out.Errors, Finding{Component: e.Component, Field: e.Field, Message: e.Message, Suggestion: e.Suggestion})
	}
	for _, w := range res.Warnings {
		out.Warnings = append(out.Warnings, Finding{Component: w.Component, Field: w.Field, Message: w.Message})
	}
	for _, g := range model.CollectGenericFallbacks(m, reg) {
		out.Warnings = append(out.Warnings, Finding{
			Component: g.Component, Field: "type",
			Message:    fmt.Sprintf("no installed provider defines type %q, so it falls back to a generic component with no facts of its own", g.Type),
			Suggestion: "check the spelling against types_list, or add the provider to meta.providers",
		})
	}
	return out, nil
}

// loadModelParam loads a model from exactly one of a server-side path or
// inline YAML.
func loadModelParam(path, source string) (*model.Model, error) {
	switch {
	case path != "" && source != "":
		return nil, fmt.Errorf("give model_path or model_source, not both")
	case path == "" && source == "":
		return nil, fmt.Errorf("model_path or model_source is required")
	case len(source) > maxModelSourceBytes:
		return nil, fmt.Errorf("model_source is %d bytes; the limit is %d", len(source), maxModelSourceBytes)
	case source != "":
		return model.LoadBytes([]byte(source), "")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("model_path %q: %w (a client that cannot write files where the server reads them passes model_source)", path, err)
	}
	return model.Load(path)
}

// ScenarioSimulateParams takes a model and the scenarios to run against
// it, each as a path the server can read or inline YAML.
type ScenarioSimulateParams struct {
	ModelPath       string `json:"model_path,omitempty"`
	ModelSource     string `json:"model_source,omitempty"`
	ScenariosPath   string `json:"scenarios_path,omitempty"`
	ScenariosSource string `json:"scenarios_source,omitempty"`
}

// Conclusion is what the engine concluded, or what a scenario expects.
type Conclusion struct {
	RootCause          string   `json:"root_cause"`
	Path               []string `json:"path,omitempty"`
	Eliminated         []string `json:"eliminated,omitempty"`
	NotEliminated      []string `json:"not_eliminated,omitempty"`
	CannotRuleOut      []string `json:"cannot_rule_out,omitempty"`
	RedundancyDegraded []string `json:"redundancy_degraded,omitempty"`
}

// ScenarioOutcome is one scenario's verdict, expected beside actual.
type ScenarioOutcome struct {
	Name     string     `json:"name"`
	Pass     bool       `json:"pass"`
	Expected Conclusion `json:"expected"`
	Actual   Conclusion `json:"actual"`
	// Error says why the scenario could not run, such as an entry the
	// model lacks.
	Error string `json:"error,omitempty"`
}

// ScenarioSimulateResult reports every scenario.
type ScenarioSimulateResult struct {
	Passed  int               `json:"passed"`
	Failed  int               `json:"failed"`
	Results []ScenarioOutcome `json:"results"`
}

// ScenarioSimulate runs scenarios against a model, as `mgtt simulate`
// does: injected facts, no probes, no live system.
func (h *Handler) ScenarioSimulate(p ScenarioSimulateParams) (*ScenarioSimulateResult, error) {
	m, err := loadModelParam(p.ModelPath, p.ModelSource)
	if err != nil {
		return nil, err
	}
	scs, err := loadScenariosParam(p.ScenariosPath, p.ScenariosSource)
	if err != nil {
		return nil, err
	}
	reg, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	out := &ScenarioSimulateResult{Results: []ScenarioOutcome{}}
	for _, sc := range scs {
		r := simulate.Run(m, reg, sc)
		if r.Pass {
			out.Passed++
		} else {
			out.Failed++
		}
		out.Results = append(out.Results, ScenarioOutcome{Name: sc.Name, Pass: r.Pass, Expected: conclusion(sc.Expect), Actual: conclusion(r.Actual), Error: r.Err})
	}
	return out, nil
}

// ScenarioSuggestParams is the input for scenario_suggest.
type ScenarioSuggestParams struct {
	ModelPath   string `json:"model_path,omitempty"`
	ModelSource string `json:"model_source,omitempty"`
	// Component keeps only the drafts for failures rooted at it.
	Component string `json:"component,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

// DraftInfo is one drafted scenario.
type DraftInfo struct {
	Name string `json:"name"`
	// Chain is the failure chain the facts were drawn from, as
	// component.state steps, and Count how many chains share its root and
	// root state; both absent for the all-healthy draft.
	Chain []string `json:"chain,omitempty"`
	Count int      `json:"count,omitempty"`
	// RootCause is the conclusion the draft's expect: records.
	RootCause string `json:"root_cause"`
	// Review is set when that conclusion is not the chain's root and no
	// redundancy group explains it.
	Review string `json:"review,omitempty"`
	// YAML is the scenario file, ready to edit and keep.
	YAML string `json:"yaml"`
}

// ScenarioSuggestResult is one page of drafts.
type ScenarioSuggestResult struct {
	Drafts []DraftInfo `json:"drafts"`
	// Chains is how many failure chains the model has; Total is how many
	// drafts there are over all pages.
	Chains        int    `json:"chains"`
	Total         int    `json:"total"`
	NextPageToken string `json:"next_page_token,omitempty"`
	// Unshowable names failures, as component.state, no facts were found
	// to show: the component's healthy rules hold in that state.
	Unshowable []string `json:"unshowable,omitempty"`
}

// draftsPerPage is scenario_suggest's default page: a storefront draft is
// a few kilobytes of YAML.
const draftsPerPage = 10

// ScenarioSuggest drafts scenarios from the model's own failure chains,
// as `mgtt simulate --suggest` does.
func (h *Handler) ScenarioSuggest(p ScenarioSuggestParams) (*ScenarioSuggestResult, error) {
	m, err := loadModelParam(p.ModelPath, p.ModelSource)
	if err != nil {
		return nil, err
	}
	reg, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	ds, err := simulate.Suggest(m, reg, simulate.SuggestOptions{Component: p.Component})
	if err != nil {
		return nil, err
	}
	limit := p.Limit
	if limit <= 0 {
		limit = draftsPerPage
	}
	start, end, next, err := page(len(ds.Drafts), limit, p.PageToken)
	if err != nil {
		return nil, err
	}
	out := &ScenarioSuggestResult{Drafts: []DraftInfo{}, Chains: ds.Chains, Total: len(ds.Drafts), NextPageToken: next, Unshowable: ds.Unshowable}
	for _, d := range ds.Drafts[start:end] {
		data, err := simulate.MarshalDraft(d, m.Order)
		if err != nil {
			return nil, err
		}
		info := DraftInfo{Name: d.Scenario.Name, Count: d.Count, RootCause: d.Scenario.Expect.RootCause, Review: d.Review, YAML: string(data)}
		for _, st := range d.Chain.Chain {
			info.Chain = append(info.Chain, st.Component+"."+st.State)
		}
		out.Drafts = append(out.Drafts, info)
	}
	return out, nil
}

func conclusion(e simulate.Expectation) Conclusion {
	return Conclusion{RootCause: e.RootCause, Path: e.Path, Eliminated: e.Eliminated, NotEliminated: e.NotEliminated, CannotRuleOut: e.CannotRuleOut, RedundancyDegraded: e.RedundancyDegraded}
}

// loadScenariosParam loads scenarios from exactly one of a server-side
// file or directory, or inline YAML (several separated by `---`).
func loadScenariosParam(path, source string) ([]*simulate.Scenario, error) {
	switch {
	case path != "" && source != "":
		return nil, fmt.Errorf("give scenarios_path or scenarios_source, not both")
	case path == "" && source == "":
		return nil, fmt.Errorf("scenarios_path or scenarios_source is required")
	case len(source) > maxModelSourceBytes:
		return nil, fmt.Errorf("scenarios_source is %d bytes; the limit is %d", len(source), maxModelSourceBytes)
	case source != "":
		return simulate.ParseScenarios([]byte(source))
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("scenarios_path %q: %w (a client that cannot write files where the server reads them passes scenarios_source)", path, err)
	}
	if info.IsDir() {
		return simulate.LoadAllScenarios(path)
	}
	sc, err := simulate.LoadScenario(path)
	if err != nil {
		return nil, err
	}
	return []*simulate.Scenario{sc}, nil
}

// ModelImpactParams names the component to fail, optionally in which
// states, in a model given by path or inline.
type ModelImpactParams struct {
	ModelPath   string   `json:"model_path,omitempty"`
	ModelSource string   `json:"model_source,omitempty"`
	Component   string   `json:"component"`
	States      []string `json:"states,omitempty"`
}

// AffectedInfo is one component the failure reaches.
type AffectedInfo struct {
	Component  string   `json:"component"`
	States     []string `json:"states"`
	Path       []string `json:"path"`
	Symptom    bool     `json:"symptom,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
}

// BlockedInfo is where redundancy stops the failure.
type BlockedInfo struct {
	Dependent string `json:"dependent"`
	Member    string `json:"member"`
}

// ModelImpactResult is the blast radius of one component's failure.
type ModelImpactResult struct {
	Component string         `json:"component"`
	States    []string       `json:"states"`
	Affected  []AffectedInfo `json:"affected"`
	Symptoms  []string       `json:"symptoms"`
	Blocked   []BlockedInfo  `json:"blocked"`
}

// ModelImpact answers "what breaks if this fails?" from the model alone.
func (h *Handler) ModelImpact(p ModelImpactParams) (*ModelImpactResult, error) {
	if p.Component == "" {
		return nil, fmt.Errorf("component is required")
	}
	m, err := loadModelParam(p.ModelPath, p.ModelSource)
	if err != nil {
		return nil, err
	}
	reg, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	imp, err := scenarios.ImpactOf(scenarios.BuildGraph(m, reg), m, p.Component, p.States)
	if err != nil {
		return nil, err
	}
	out := &ModelImpactResult{Component: imp.Component, States: imp.States, Affected: []AffectedInfo{}, Symptoms: []string{}, Blocked: []BlockedInfo{}}
	for _, a := range imp.Affected {
		out.Affected = append(out.Affected, AffectedInfo{Component: a.Component, States: a.States, Path: a.Path, Symptom: a.Symptom, Conditions: a.Conditions})
		if a.Symptom {
			out.Symptoms = append(out.Symptoms, a.Component)
		}
	}
	for _, b := range imp.Blocked {
		out.Blocked = append(out.Blocked, BlockedInfo{Dependent: b.Dependent, Member: b.Member})
	}
	return out, nil
}

// ModelDiffParams takes two revisions of a model, each by path or inline.
type ModelDiffParams struct {
	OldModelPath   string `json:"old_model_path,omitempty"`
	OldModelSource string `json:"old_model_source,omitempty"`
	NewModelPath   string `json:"new_model_path,omitempty"`
	NewModelSource string `json:"new_model_source,omitempty"`
}

// ComponentChange is one component's changes, as review lines.
type ComponentChange struct {
	Component string   `json:"component"`
	Changes   []string `json:"changes"`
}

// ReachInfo is how the symptoms a component's failure reaches changed.
type ReachInfo struct {
	Component string   `json:"component"`
	Gained    []string `json:"gained,omitempty"`
	Lost      []string `json:"lost,omitempty"`
}

// ModelDiffResult is the semantic difference between two revisions.
type ModelDiffResult struct {
	Same         bool              `json:"same"`
	Added        []string          `json:"added"`
	Removed      []string          `json:"removed"`
	Changed      []ComponentChange `json:"changed"`
	Reach        []ReachInfo       `json:"reach"`
	OldScenarios int               `json:"old_scenarios"`
	NewScenarios int               `json:"new_scenarios"`
}

// ModelDiff compares two model revisions by meaning.
func (h *Handler) ModelDiff(p ModelDiffParams) (*ModelDiffResult, error) {
	oldM, err := loadModelParam(p.OldModelPath, p.OldModelSource)
	if err != nil {
		return nil, fmt.Errorf("old model: %w", err)
	}
	newM, err := loadModelParam(p.NewModelPath, p.NewModelSource)
	if err != nil {
		return nil, fmt.Errorf("new model: %w", err)
	}
	reg, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	d := modeldiff.Compare(oldM, newM, reg)
	out := &ModelDiffResult{Same: d.Empty(), Added: nonNil(d.Added), Removed: nonNil(d.Removed), Changed: []ComponentChange{}, Reach: []ReachInfo{}, OldScenarios: d.OldScenarios, NewScenarios: d.NewScenarios}
	for _, c := range d.Changed {
		out.Changed = append(out.Changed, ComponentChange{Component: c.Name, Changes: c.Changes})
	}
	for _, r := range d.Reach {
		out.Reach = append(out.Reach, ReachInfo{Component: r.Component, Gained: r.Gained, Lost: r.Lost})
	}
	return out, nil
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}
