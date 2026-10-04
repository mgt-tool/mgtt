// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/simulate"
)

// authoringHandler installs the test write provider (type `service`, fact
// `status`, healthy `status == healthy`) into a fresh MGTT_HOME.
func authoringHandler(t *testing.T) *Handler {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	installWriteProvider(t, home)
	return NewHandler(Config{Toolset: "authoring"})
}

func TestTypesListAndDescribe(t *testing.T) {
	h := authoringHandler(t)
	list, err := h.TypesList(TypesListParams{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ty := range list.Types {
		if ty.Provider == "testwriter" && ty.Type == "service" && ty.Facts == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("testwriter/service missing from %+v", list.Types)
	}
	d, err := h.TypesDescribe(TypesDescribeParams{Type: "service"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Provider != "testwriter" || len(d.Facts) != 1 || d.Facts[0].Name != "status" || d.Facts[0].Type != "mgtt.string" {
		t.Fatalf("facts: %+v", d)
	}
	if len(d.Healthy) != 1 || d.Healthy[0] != "status == healthy" || d.DefaultActiveState != "live" || len(d.States) != 2 {
		t.Fatalf("rules/states: %+v", d)
	}
	if _, err := h.TypesDescribe(TypesDescribeParams{Type: "nope"}); err == nil || !strings.Contains(err.Error(), "types_list") {
		t.Fatalf("unknown type should point at types_list, got %v", err)
	}
}

// A draft passed inline is validated whole: every finding in one report,
// including the warnings a modeller most needs.
func TestModelValidate_InlineReportsEverything(t *testing.T) {
	h := authoringHandler(t)
	src := `meta:
  name: draft
  version: "1"
  providers: [testwriter]
components:
  api:
    type: service
    healthy: [status != degraded]
    depends:
      - on: ghost
  web:
    type: servce
`
	res, err := h.ModelValidate(ModelValidateParams{ModelSource: src})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Components != 2 {
		t.Fatalf("want a failing report on 2 components, got %+v", res)
	}
	has := func(fs []Finding, comp, field, text string) bool {
		for _, f := range fs {
			if f.Component == comp && f.Field == field && strings.Contains(f.Message, text) {
				return true
			}
		}
		return false
	}
	if !has(res.Errors, "api", "depends", "ghost") {
		t.Errorf("missing unknown-dependency error: %+v", res.Errors)
	}
	if !has(res.Warnings, "api", "healthy", `"status == healthy"`) {
		t.Errorf("missing dropped-rule warning: %+v", res.Warnings)
	}
	if !has(res.Warnings, "web", "type", `"servce"`) {
		t.Errorf("missing unknown-type warning: %+v", res.Warnings)
	}
}

func TestModelValidate_ExactlyOneSource(t *testing.T) {
	h := authoringHandler(t)
	for name, p := range map[string]ModelValidateParams{
		"neither": {},
		"both":    {ModelPath: "x.yaml", ModelSource: "meta: {}"},
		"too big": {ModelSource: strings.Repeat("#", maxModelSourceBytes+1)},
	} {
		if _, err := h.ModelValidate(p); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// --toolset decides what is served: the authoring tools reach no live
// system, so a client given only them cannot start an incident or probe.
func TestToolsets(t *testing.T) {
	for set, want := range map[string]struct{ authoring, diagnose bool }{
		"":          {true, true},
		"all":       {true, true},
		"authoring": {true, false},
		"diagnose":  {false, true},
	} {
		tools := buildServer(Config{Toolset: set}).ListTools()
		if _, ok := tools["model_validate"]; ok != want.authoring {
			t.Errorf("toolset %q: model_validate served = %v", set, ok)
		}
		if _, ok := tools["probe"]; ok != want.diagnose {
			t.Errorf("toolset %q: probe served = %v", set, ok)
		}
		if _, ok := tools["about"]; !ok {
			t.Errorf("toolset %q: about must always be served", set)
		}
	}
	if err := checkToolset("everything"); err == nil {
		t.Error("an unknown toolset must be rejected")
	}
	if a, _ := NewHandler(Config{Toolset: "authoring"}).About(); a.Toolset != "authoring" {
		t.Errorf("about.toolset = %q", a.Toolset)
	}
}

// Scenarios sent inline as YAML documents run against an inline model;
// each comes back with its verdict and the conclusion beside the expected.
func TestScenarioSimulate_Inline(t *testing.T) {
	h := authoringHandler(t)
	model := "meta:\n  name: s\n  version: \"1\"\n  providers: [testwriter]\ncomponents:\n  api:\n    type: service\n"
	scenarios := `name: api broken
inject:
  api: { status: degraded }
expect:
  root_cause: api
---
name: wrongly expects healthy
inject:
  api: { status: degraded }
expect:
  root_cause: none
`
	res, err := h.ScenarioSimulate(ScenarioSimulateParams{ModelSource: model, ScenariosSource: scenarios})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed != 1 || res.Failed != 1 || len(res.Results) != 2 {
		t.Fatalf("want 1 pass, 1 fail; got %+v", res)
	}
	bad := res.Results[1]
	if bad.Pass || bad.Expected.RootCause != "none" || bad.Actual.RootCause != "api" {
		t.Fatalf("failing scenario should show expected none beside actual api: %+v", bad)
	}
	if _, err := h.ScenarioSimulate(ScenarioSimulateParams{ModelSource: model}); err == nil {
		t.Error("scenarios are required")
	}
}

// The guide serves its index by default, every listed topic, and names
// the topics when asked for one that does not exist.
func TestGuide(t *testing.T) {
	h := NewHandler(Config{})
	idx, err := h.Guide(GuideParams{})
	if err != nil {
		t.Fatal(err)
	}
	if idx.Topic != "index" || !strings.Contains(idx.Text, "authoring loop") {
		t.Fatalf("index: %+v", idx.Topic)
	}
	for _, topic := range idx.Topics {
		if !strings.Contains(idx.Text, "`"+topic+"`") && topic != "index" {
			t.Errorf("index does not list topic %q", topic)
		}
		if _, err := h.Guide(GuideParams{Topic: topic}); err != nil {
			t.Errorf("topic %q: %v", topic, err)
		}
	}
	// The scenario example teaches the format; it must parse as one.
	sa, _ := h.Guide(GuideParams{Topic: "scenarios.authoring"})
	block := sa.Text[strings.Index(sa.Text, "```yaml\n")+len("```yaml\n"):]
	block = block[:strings.Index(block, "```")]
	scs, err := simulate.ParseScenarios([]byte(block))
	if err != nil || len(scs) != 1 || len(scs[0].Unresolved) != 1 || len(scs[0].Expect.CannotRuleOut) != 1 {
		t.Errorf("scenarios.authoring example does not parse as its fields say: %v %+v", err, scs)
	}
	if _, err := h.Guide(GuideParams{Topic: "nope"}); err == nil || !strings.Contains(err.Error(), "model.health") {
		t.Errorf("unknown topic should list topics, got %v", err)
	}
}
