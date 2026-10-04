// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import (
	"reflect"
	"testing"

	"github.com/mgt-tool/mgtt/internal/model"
)

// In the diamond, entry needs one of left or right, both reading base.
func TestImpactOf(t *testing.T) {
	m, reg := diamondWithGroup()
	g := BuildGraph(m, reg)

	// base under both members: the group cannot hold, entry breaks.
	imp, err := ImpactOf(g, m, "base", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, a := range imp.Affected {
		got[a.Component] = a.Path
	}
	if !reflect.DeepEqual(got["entry"], []string{"base", "left", "entry"}) || len(imp.Blocked) != 0 {
		t.Fatalf("base: affected %v, blocked %v", got, imp.Blocked)
	}

	// left alone: right keeps entry served, so the walk stops there.
	imp, err = ImpactOf(g, m, "left", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Affected) != 0 || !reflect.DeepEqual(imp.Blocked, []Blocked{{Dependent: "entry", Member: "left"}}) {
		t.Fatalf("left: affected %+v, blocked %+v", imp.Affected, imp.Blocked)
	}
}

// A state is reached when the failure's can_cause labels trigger it --
// broken by base's `gone`, slow because it declares no triggered_by and
// so accepts any label, as enumeration does -- and a named state must be
// one of the component's failure states.
func TestImpactOf_States(t *testing.T) {
	m, reg := diamondWithGroup()
	g := BuildGraph(m, reg)
	imp, err := ImpactOf(g, m, "base", []string{"stopped"})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range imp.Affected {
		if a.Component == "left" && !reflect.DeepEqual(a.States, []string{"broken", "slow"}) {
			t.Errorf("left reached in %v, want broken and slow", a.States)
		}
	}
	if _, err := ImpactOf(g, m, "base", []string{"melted"}); err == nil {
		t.Error("an unknown state must be rejected, naming the real ones")
	}
	if _, err := ImpactOf(g, m, "nope", nil); err == nil {
		t.Error("an unknown component must be rejected")
	}
}

// Behind while: guards, a component breaks unconditionally when some
// chain crosses no guard, and otherwise only while one of the guards into
// it holds: a shared database breaks svc whichever color is selected, a
// dead idle color only while it is selected.
func TestImpactOf_Guards(t *testing.T) {
	m, reg := diamondWithGroup()
	// Make entry active/passive over left and right instead of a group.
	m.Components["entry"].Depends = []model.Dependency{
		{On: []string{"left"}, WhileRaw: "color == left"},
		{On: []string{"right"}, WhileRaw: "color == right"},
	}
	g := BuildGraph(m, reg)
	cond := func(root string) []string {
		imp, err := ImpactOf(g, m, root, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range imp.Affected {
			if a.Component == "entry" {
				return a.Conditions
			}
		}
		t.Fatalf("%s: entry not affected", root)
		return nil
	}
	if got := cond("base"); len(got) != 2 {
		t.Errorf("base reaches entry through both guards; got %q", got)
	}
	if got := cond("left"); !reflect.DeepEqual(got, []string{"entry depends on left while color == left"}) {
		t.Errorf("left reaches entry only while it is selected; got %q", got)
	}
}
