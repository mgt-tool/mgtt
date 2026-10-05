// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"slices"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// groupModel: svc needs one of the apps a and b (need: 1), both apps read
// db, and log stands alone with a type that has no healthy rules, so no
// facts can show it failing.
func groupModel(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()
	parse := func(s string) expr.Node {
		n, err := expr.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	upDown := func(name string, rules bool) *providersupport.Type {
		ty := &providersupport.Type{
			Name:  name,
			Facts: map[string]*providersupport.FactSpec{"up": {TypeName: "mgtt.bool"}},
			States: []providersupport.StateDef{
				{Name: "live", When: parse("up == true")},
				{Name: "stopped", When: parse("up == false")},
			},
			DefaultActiveState: "live",
			FailureModes:       map[string][]string{"stopped": {"upstream_failure"}},
		}
		if rules {
			ty.Healthy = []expr.Node{parse("up == true")}
			ty.HealthyRaw = []string{"up == true"}
		}
		return ty
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{
			"svc": upDown("svc", true), "app": upDown("app", true),
			"db": upDown("db", true), "log": upDown("log", false),
		},
	})
	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"svc": {Name: "svc", Type: "svc", Depends: []model.Dependency{{On: []string{"a", "b"}, Need: 1}}},
			"a":   {Name: "a", Type: "app", Depends: []model.Dependency{{On: []string{"db"}}}},
			"b":   {Name: "b", Type: "app", Depends: []model.Dependency{{On: []string{"db"}}}},
			"db":  {Name: "db", Type: "db"},
			"log": {Name: "log", Type: "log"},
		},
		Order: []string{"svc", "a", "b", "db", "log"},
	}
	m.BuildGraph()
	return m, reg
}

func draftNamed(t *testing.T, ds *Drafts, name string) Draft {
	t.Helper()
	for _, d := range ds.Drafts {
		if d.Scenario.Name == name {
			return d
		}
	}
	var names []string
	for _, d := range ds.Drafts {
		names = append(names, d.Scenario.Name)
	}
	t.Fatalf("no draft %q among %v", name, names)
	return Draft{}
}

func TestSuggest_DraftsPassAsWritten(t *testing.T) {
	m, reg := groupModel(t)
	ds, err := Suggest(m, reg, SuggestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ds.Chains == 0 || len(ds.Drafts) < 2 {
		t.Fatalf("got %d drafts from %d chains", len(ds.Drafts), ds.Chains)
	}
	if got := draftNamed(t, ds, "all healthy").Scenario.Expect.RootCause; got != "none" {
		t.Errorf("all healthy: root cause %q, want none", got)
	}
	for _, d := range ds.Drafts {
		if r := Run(m, reg, d.Scenario); !r.Pass {
			t.Errorf("%s: fails as drafted: expected %+v, got %+v", d.Scenario.Name, d.Scenario.Expect, r.Actual)
		}
		if d.Review != "" {
			t.Errorf("%s: unexpected review note %q", d.Scenario.Name, d.Review)
		}
	}
}

// One member down while the other holds is the redundancy archetype: no
// root cause, the member reported degraded.
func TestSuggest_AMemberAloneIsRedundancyDegraded(t *testing.T) {
	m, reg := groupModel(t)
	ds, _ := Suggest(m, reg, SuggestOptions{})
	d := draftNamed(t, ds, "a stopped")
	if e := d.Scenario.Expect; e.RootCause != "none" || !slices.Equal(e.RedundancyDegraded, []string{"a"}) {
		t.Errorf("got %+v, want no root cause and a degraded", e)
	}
	if d.Scenario.Inject["b"]["up"] != true {
		t.Errorf("b is out of a's reach, so it should be injected healthy; got %v", d.Scenario.Inject["b"])
	}
}

// The database under both apps breaks the group: it is the root cause. The
// app off the drafted chain is in the failure's reach, so it is left
// unknown, as mid-incident.
func TestSuggest_ACauseUnderTheGroup(t *testing.T) {
	m, reg := groupModel(t)
	ds, _ := Suggest(m, reg, SuggestOptions{})
	d := draftNamed(t, ds, "db stopped")
	if d.Scenario.Expect.RootCause != "db" {
		t.Fatalf("root cause %q, want db", d.Scenario.Expect.RootCause)
	}
	onChain := 0
	for _, app := range []string{"a", "b"} {
		if _, ok := d.Scenario.Inject[app]; ok {
			onChain++
		}
	}
	if onChain != 1 || d.Scenario.Inject["svc"] == nil || d.Count < 2 {
		t.Errorf("want db, one app and svc injected for a chain standing for several; got %v (count %d)", d.Scenario.Inject, d.Count)
	}
}

func TestSuggest_UnshowableFailure(t *testing.T) {
	m, reg := groupModel(t)
	ds, _ := Suggest(m, reg, SuggestOptions{})
	if !slices.Contains(ds.Unshowable, "log.stopped") {
		t.Errorf("log has no healthy rules, so nothing shows it failing; unshowable = %v", ds.Unshowable)
	}
	for _, d := range ds.Drafts {
		if strings.HasPrefix(d.Scenario.Name, "log ") {
			t.Errorf("drafted %q for a failure no facts can show", d.Scenario.Name)
		}
	}
}

func TestSuggest_OneComponent(t *testing.T) {
	m, reg := groupModel(t)
	ds, err := Suggest(m, reg, SuggestOptions{Component: "db"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Drafts) != 1 || ds.Drafts[0].Scenario.Name != "db stopped" {
		t.Errorf("got %d drafts, want db stopped alone", len(ds.Drafts))
	}
	if _, err := Suggest(m, reg, SuggestOptions{Component: "ghost"}); err == nil {
		t.Error("a component the model lacks should be an error")
	}
}

func TestMarshalDraft_ReadsBackAndPasses(t *testing.T) {
	m, reg := groupModel(t)
	ds, _ := Suggest(m, reg, SuggestOptions{})
	d := draftNamed(t, ds, "db stopped")
	d.Review = "a note for the author"
	data, err := MarshalDraft(d, m.Order)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "  db: {up: false}") || !strings.Contains(text, "# REVIEW: a note for the author.") {
		t.Errorf("want one line of facts per component and the review note:\n%s", text)
	}
	scs, err := ParseScenarios(data)
	if err != nil || len(scs) != 1 {
		t.Fatalf("parse back: %v (%d scenarios)\n%s", err, len(scs), text)
	}
	if r := Run(m, reg, scs[0]); !r.Pass {
		t.Errorf("read back, the draft fails: expected %+v, got %+v", scs[0].Expect, r.Actual)
	}
}

// A failure only another top-level component shows is drafted from there:
// the draft names that component as its entry, and the engine names the
// failure.
func TestSuggest_SeenAtAnotherEntry(t *testing.T) {
	m, reg := groupModel(t)
	m.Components["cron"] = &model.Component{Name: "cron", Type: "app"}
	m.Order = append(m.Order, "cron")
	m.BuildGraph()
	ds, _ := Suggest(m, reg, SuggestOptions{Component: "cron"})
	if len(ds.Drafts) != 1 {
		t.Fatalf("got %d drafts, want cron stopped", len(ds.Drafts))
	}
	d := ds.Drafts[0]
	if d.Scenario.Entry != "cron" || d.Scenario.Expect.RootCause != "cron" || d.Review != "" {
		t.Errorf("got entry %q, root cause %q, review %q; want cron, cron, none", d.Scenario.Entry, d.Scenario.Expect.RootCause, d.Review)
	}
	data, _ := MarshalDraft(d, m.Order)
	if !strings.Contains(string(data), "\nentry: cron\n") {
		t.Errorf("the entry belongs in the file:\n%s", data)
	}
}

func TestReview(t *testing.T) {
	for _, c := range []struct {
		got  Expectation
		want string
	}{
		{Expectation{RootCause: "db"}, ""},
		{Expectation{RootCause: "none", RedundancyDegraded: []string{"db"}}, ""},
		{Expectation{RootCause: "none"}, "the engine names none from these facts, not db"},
		{Expectation{RootCause: "api"}, "the engine names api from these facts, not db"},
	} {
		if got := review("db", c.got); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.got, got, c.want)
		}
	}
}

func TestRun_AnEntryTheModelLacksFails(t *testing.T) {
	m, reg := groupModel(t)
	r := Run(m, reg, &Scenario{Name: "x", Entry: "ghost", Expect: Expectation{RootCause: "none"}})
	if r.Pass || !strings.Contains(r.Err, `"ghost"`) {
		t.Errorf("got pass %v, err %q; want a failure naming the entry", r.Pass, r.Err)
	}
}
