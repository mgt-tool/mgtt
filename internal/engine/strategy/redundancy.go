// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// SatisfiedGroup reports whether parent's dependency on child is a
// redundancy group (need: k) that still has at least k members proven
// healthy. A satisfied group does not pass a member's failure on to
// parent. Members whose health is unknown do not count toward k, so an
// unread fact never hides a failure. The second result describes the
// group for a reason string.
func SatisfiedGroup(m *model.Model, reg *providersupport.Registry, store *facts.Store, parent, child string) (bool, string) {
	comp := m.Components[parent]
	if comp == nil {
		return false, ""
	}
	for _, dep := range comp.Depends {
		if dep.Need <= 0 || !contains(dep.On, child) {
			continue
		}
		healthy := 0
		for _, member := range dep.On {
			if ComponentVerdict(m, reg, store, member) == Healthy {
				healthy++
			}
		}
		if healthy >= dep.Need {
			return true, fmt.Sprintf("%s: %d of %d healthy in [%s], needs %d", parent, healthy, len(dep.On), strings.Join(dep.On, ", "), dep.Need)
		}
	}
	return false, ""
}

// RedundancyCovered reports whether every component that depends on name
// does so through a satisfied group: name may be broken, but nothing it
// serves is broken by it.
func RedundancyCovered(m *model.Model, reg *providersupport.Registry, store *facts.Store, name string) bool {
	dependents := 0
	for _, parent := range m.Order {
		comp := m.Components[parent]
		if comp == nil || !dependsOn(comp, name) {
			continue
		}
		dependents++
		if ok, _ := SatisfiedGroup(m, reg, store, parent, name); !ok {
			return false
		}
	}
	return dependents > 0
}

// RedundancyDegraded lists, in model order, the components seen broken
// that redundancy covers: not a root cause, but not healthy either, and a
// conclusion that leaves them out would call a degraded system healthy.
func RedundancyDegraded(m *model.Model, reg *providersupport.Registry, store *facts.Store) []string {
	if m == nil || store == nil {
		return nil
	}
	var out []string
	for _, name := range m.Order {
		if ComponentVerdict(m, reg, store, name) != Unhealthy || !RedundancyCovered(m, reg, store, name) {
			continue
		}
		var why []string
		for _, parent := range m.Order {
			if comp := m.Components[parent]; comp != nil && dependsOn(comp, name) {
				_, desc := SatisfiedGroup(m, reg, store, parent, name)
				why = append(why, desc)
			}
		}
		sort.Strings(why)
		out = append(out, name+" ("+strings.Join(why, "; ")+")")
	}
	return out
}

func dependsOn(comp *model.Component, name string) bool {
	for _, dep := range comp.Depends {
		if contains(dep.On, name) {
			return true
		}
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
