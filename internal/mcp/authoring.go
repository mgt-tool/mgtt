// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"fmt"
	"os"
	"sort"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
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
		out.Results = append(out.Results, ScenarioOutcome{Name: sc.Name, Pass: r.Pass, Expected: conclusion(sc.Expect), Actual: conclusion(r.Actual)})
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
