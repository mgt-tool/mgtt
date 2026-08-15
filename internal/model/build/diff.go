// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package build

import (
	"sort"

	"github.com/mgt-tool/mgtt/internal/model"
)

// Diff describes the changes between an existing committed model and
// a freshly built one.
type Diff struct {
	Added       []string     // new component names (sorted)
	Removed     []string     // removed component names (sorted)
	TypeChanged []TypeChange // components whose type field changed
}

// TypeChange records a component whose type differs between prev and
// next. Surfaced distinctly from Add/Remove because renaming a type
// can mean the probe surface changed underfoot — worth human review
// even though component-count is unchanged.
type TypeChange struct {
	Name string
	From string
	To   string
}

// HasDeletions reports whether the diff contains any removals — the
// primary signal for the safety gate in the next task.
func (d Diff) HasDeletions() bool { return len(d.Removed) > 0 }

// ComputeDiff compares two models component-by-component. Either side
// may be nil: nil prev = everything is an addition; nil next = empty
// model (all prev components removed).
func ComputeDiff(prev, next *model.Model) Diff {
	prevComps := components(prev)
	nextComps := components(next)
	var d Diff
	for name, c := range nextComps {
		prevC, existed := prevComps[name]
		if !existed {
			d.Added = append(d.Added, name)
			continue
		}
		if prevC.Type != c.Type {
			d.TypeChanged = append(d.TypeChanged, TypeChange{Name: name, From: prevC.Type, To: c.Type})
		}
	}
	for name := range prevComps {
		if _, ok := nextComps[name]; !ok {
			d.Removed = append(d.Removed, name)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	sort.Slice(d.TypeChanged, func(i, j int) bool { return d.TypeChanged[i].Name < d.TypeChanged[j].Name })
	return d
}

// components returns m.Components, treating nil as empty so callers
// don't need a branch. Iterating the live map directly is cheaper than
// copying into a scratch map (the allocations ComputeDiff used to do
// were pure ceremony).
func components(m *model.Model) map[string]*model.Component {
	if m == nil {
		return nil
	}
	return m.Components
}
