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
