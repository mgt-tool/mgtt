// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// diamondWithGroup: entry needs one of left or right; both read base. A
// redundancy group, triggered_by labels, intermediate terminals and a
// common cause: the cases the graph must carry for Read to give back
// exactly what Enumerate gives.
func diamondWithGroup() (*model.Model, *providersupport.Registry) {
	st := func(name string, by ...string) providersupport.StateDef {
		return providersupport.StateDef{Name: name, TriggeredBy: by}
	}
	reg := buildRegistry("p", map[string]*providersupport.Type{
		"entry": testType("entry", "live", []string{"e"}, []providersupport.StateDef{st("down", "fail"), st("live")}, map[string][]string{"down": {"outage"}}),
		"mid":   testType("mid", "live", []string{"m1", "m2"}, []providersupport.StateDef{st("broken", "gone"), st("slow"), st("live")}, map[string][]string{"broken": {"fail"}, "slow": {"fail", "lag"}}),
		"base":  testType("base", "live", []string{"b"}, []providersupport.StateDef{st("stopped"), st("live")}, map[string][]string{"stopped": {"gone"}}),
	})
	m := buildModel([]string{"p"},
		map[string]string{"entry": "entry", "left": "mid", "right": "mid", "base": "base"},
		map[string][]string{"left": {"base"}, "right": {"base"}})
	m.Components["entry"].Depends = []model.Dependency{{On: []string{"left", "right"}, Need: 1}}
	return m, reg
}

// The graph is all enumeration reads, so writing it and reading it back
// gives exactly the scenarios Enumerate does, IDs and order included.
func TestGraph_RoundTripEqualsEnumerate(t *testing.T) {
	m, reg := diamondWithGroup()
	want := Enumerate(m, reg)
	if len(want) == 0 {
		t.Fatal("fixture enumerates nothing")
	}
	var b bytes.Buffer
	if err := WriteGraph(&b, "sha256:x", BuildGraph(m, reg)); err != nil {
		t.Fatal(err)
	}
	got, hash, err := Read(&b)
	if err != nil {
		t.Fatal(err)
	}
	if hash != "sha256:x" {
		t.Errorf("source_hash = %q", hash)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read back %d scenarios, Enumerate gives %d; they differ", len(got), len(want))
	}
}

// The group carries the common cause to entry and nothing else: base
// fails both members, one member alone leaves entry served.
func TestGraph_GroupLinkCarriesOnlyCommonCause(t *testing.T) {
	m, reg := diamondWithGroup()
	g := BuildGraph(m, reg)
	link := g.Components["left"].Dependents[0]
	if !link.Group || !reflect.DeepEqual(link.Roots, []string{"base"}) {
		t.Fatalf("left -> entry link = %+v, want a group link carrying only base", link)
	}
}

// A file this version does not know is an error, not an empty set that
// would quietly turn scenario-guided diagnosis off.
func TestRead_UnknownFormat(t *testing.T) {
	_, _, err := Read(strings.NewReader("source_hash: x\nformat: graph/v9\n"))
	if err == nil || !strings.Contains(err.Error(), "graph/v9") {
		t.Fatalf("want an unknown-format error, got %v", err)
	}
}

// A failure every dependent ignores -- its failure states all name other
// triggered_by labels -- is still a chain: one step, seen at the root.
func TestExpand_FailureNoDependentAnswersTo(t *testing.T) {
	g := &Graph{Components: map[string]*GraphComponent{
		"worker": {
			Observes:   []string{"up"},
			States:     []GraphState{{Name: "stopped", Emits: []string{"upstream_failure"}}},
			Dependents: []GraphLink{{To: "job"}},
		},
		"job": {
			Observes: []string{"up"},
			States:   []GraphState{{Name: "stopped", TriggeredBy: []string{"nothing_emits_this"}}},
		},
	}}
	var got []string
	for _, s := range Expand(g) {
		if s.Root.Component == "worker" {
			got = append(got, s.Terminal())
		}
	}
	if len(got) != 1 || got[0] != "worker" {
		t.Fatalf("worker.stopped chains end at %v; want one chain ending at worker", got)
	}
}

// An ID names a chain by its content, so a change elsewhere in the model
// leaves it alone: adding an unrelated component renumbered every chain
// sorted after it, when IDs were positions.
func TestExpand_IDsSurviveUnrelatedChanges(t *testing.T) {
	g := &Graph{Components: map[string]*GraphComponent{
		"db":  {Observes: []string{"up"}, States: []GraphState{{Name: "down", Emits: []string{"x"}}}, Dependents: []GraphLink{{To: "api"}}},
		"api": {Observes: []string{"up"}, States: []GraphState{{Name: "down"}}},
	}}
	before := map[string]string{}
	for _, s := range Expand(g) {
		before[chainKey(s.Chain)] = s.ID
		if len(s.ID) != len("s-")+12 || s.ID[:2] != "s-" {
			t.Errorf("ID %q: want s- and 12 hex digits", s.ID)
		}
	}
	g.Components["aaa-cache"] = &GraphComponent{Observes: []string{"up"}, States: []GraphState{{Name: "down"}}}
	for _, s := range Expand(g) {
		if id, ok := before[chainKey(s.Chain)]; ok && id != s.ID {
			t.Errorf("%s: ID %s became %s after adding an unrelated component", chainKey(s.Chain), id, s.ID)
		}
	}
}

func TestExpand_NoChainTwice(t *testing.T) {
	g := &Graph{Components: map[string]*GraphComponent{
		"db":  {Observes: []string{"up"}, States: []GraphState{{Name: "down", Emits: []string{"x"}}}, Dependents: []GraphLink{{To: "api"}}},
		"api": {Observes: []string{"up"}, States: []GraphState{{Name: "down"}}},
	}}
	seen := map[string]bool{}
	for _, s := range Expand(g) {
		if seen[chainKey(s.Chain)] {
			t.Errorf("%s enumerated twice", chainKey(s.Chain))
		}
		seen[chainKey(s.Chain)] = true
	}
}
