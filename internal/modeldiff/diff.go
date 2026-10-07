// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package modeldiff compares two revisions of a model by what they mean:
// structure, effective health rules, and -- what a text diff cannot show
// -- which user-facing symptoms each component's failure reaches.
package modeldiff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// Diff is the semantic difference from Old to New.
type Diff struct {
	Added, Removed []string    // components
	Changed        []Component // components in both whose meaning changed
	// Reach lists components whose failure reaches a different set of
	// user-facing symptoms: the change in what breaks for users.
	Reach                      []ReachChange
	OldScenarios, NewScenarios int
}

// Component is one component's changes, each a human-readable line.
type Component struct {
	Name    string
	Changes []string
}

// ReachChange is how the symptoms a component's failure reaches changed.
type ReachChange struct {
	Component      string
	Gained, Lost   []string
	OldAll, NewAll []string
}

// Empty reports whether the two revisions mean the same.
func (d *Diff) Empty() bool {
	return len(d.Added)+len(d.Removed)+len(d.Changed)+len(d.Reach) == 0 && d.OldScenarios == d.NewScenarios
}

// Compare diffs old against new, both resolved against reg.
func Compare(old, new *model.Model, reg *providersupport.Registry) *Diff {
	d := &Diff{}
	og, ng := scenarios.BuildGraph(old, reg), scenarios.BuildGraph(new, reg)
	d.OldScenarios, d.NewScenarios = scenarios.CountChains(og), scenarios.CountChains(ng)

	for _, n := range new.Order {
		if old.Components[n] == nil {
			d.Added = append(d.Added, n)
		}
	}
	for _, n := range old.Order {
		if new.Components[n] == nil {
			d.Removed = append(d.Removed, n)
			continue
		}
		if ch := componentChanges(old, new, reg, n); len(ch) > 0 {
			d.Changed = append(d.Changed, Component{Name: n, Changes: ch})
		}
	}

	oldReach, newReach := symptomsReached(og, old), symptomsReached(ng, new)
	for _, n := range new.Order {
		if old.Components[n] == nil {
			continue
		}
		o, nw := oldReach[n], newReach[n]
		gained, lost := minus(nw, o), minus(o, nw)
		if len(gained)+len(lost) > 0 {
			d.Reach = append(d.Reach, ReachChange{Component: n, Gained: gained, Lost: lost, OldAll: o, NewAll: nw})
		}
	}
	return d
}

func componentChanges(old, new *model.Model, reg *providersupport.Registry, name string) []string {
	oc, nc := old.Components[name], new.Components[name]
	var out []string
	if oc.Type != nc.Type {
		out = append(out, fmt.Sprintf("type %s → %s", oc.Type, nc.Type))
	}
	if oc.Resource != nc.Resource {
		out = append(out, fmt.Sprintf("resource %q → %q", oc.Resource, nc.Resource))
	}
	oe, ne := edges(oc), edges(nc)
	for _, e := range minus(ne, oe) {
		out = append(out, "depends on "+e+" (added)")
	}
	for _, e := range minus(oe, ne) {
		out = append(out, "depends on "+e+" (removed)")
	}
	or, nr := rules(old, reg, oc), rules(new, reg, nc)
	for _, k := range minus(keys(nr), keys(or)) {
		out = append(out, "healthy requires "+nr[k]+" (added)")
	}
	for _, k := range minus(keys(or), keys(nr)) {
		out = append(out, "healthy no longer requires "+or[k])
	}
	for _, k := range sortedKeys(oc.Vars, nc.Vars) {
		ov, oOK := oc.Vars[k]
		nv, nOK := nc.Vars[k]
		switch {
		case !oOK:
			out = append(out, fmt.Sprintf("var %s = %q (added)", k, nv))
		case !nOK:
			out = append(out, fmt.Sprintf("var %s (removed, was %q)", k, ov))
		case ov != nv:
			out = append(out, fmt.Sprintf("var %s %q → %q", k, ov, nv))
		}
	}
	return out
}

// edges renders c's dependencies one line per target, with the guard or
// group it depends through, so a change to either shows as a change.
func edges(c *model.Component) []string {
	var out []string
	for _, dep := range c.Depends {
		for _, on := range dep.On {
			e := on
			if dep.Need > 0 {
				e += fmt.Sprintf(" (need %d of [%s])", dep.Need, strings.Join(dep.On, ", "))
			}
			if dep.WhileRaw != "" {
				e += " while " + dep.WhileRaw
			}
			out = append(out, e)
		}
	}
	return out
}

// rules are c's effective healthy rules -- the type's, overridden or
// added to -- keyed with whitespace removed, so a respelling is no
// change, and mapped to the rule as written.
func rules(m *model.Model, reg *providersupport.Registry, c *model.Component) map[string]string {
	t, _, err := c.ResolveType(m, reg)
	if err != nil {
		t = nil
	}
	out := map[string]string{}
	for _, r := range c.HealthyRulesRaw(t) {
		out[strings.Join(strings.Fields(r), "")] = r
	}
	return out
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// symptomsReached maps each component to the sorted symptoms its failure
// reaches.
func symptomsReached(g *scenarios.Graph, m *model.Model) map[string][]string {
	out := map[string][]string{}
	for _, n := range m.Order {
		imp, err := scenarios.ImpactOf(g, m, n, nil)
		if err != nil {
			continue
		}
		var s []string
		for _, a := range imp.Affected {
			if a.Symptom {
				s = append(s, a.Component)
			}
		}
		sort.Strings(s)
		out[n] = s
	}
	return out
}

// minus returns the elements of a not in b, sorted.
func minus(a, b []string) []string {
	in := map[string]bool{}
	for _, x := range b {
		in[x] = true
	}
	var out []string
	for _, x := range a {
		if !in[x] {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(a, b map[string]string) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
