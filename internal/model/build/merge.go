// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package build

import (
	"sort"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/model"
)

// MergePrev carries over from prev what discovery cannot produce:
// every authored component (any without source: discovered -- business
// processes, external services, hand-written wiring), tombstoned
// components, and hand-authored augmentations (healthy, failure_modes,
// vars, while-guards) on kept ones. Returns the authored components kept
// although discovery did not return them, sorted.
func MergePrev(prev, next *model.Model, tombstone []string) []string {
	if prev == nil {
		return nil
	}
	var kept []string
	for name, pc := range prev.Components {
		if _, discovered := next.Components[name]; discovered || !pc.Authored() {
			continue
		}
		next.Components[name] = pc
		kept = append(kept, name)
	}
	sort.Strings(kept)
	for _, name := range tombstone {
		pc, ok := prev.Components[name]
		if !ok {
			continue
		}
		if _, alreadyInNext := next.Components[name]; alreadyInNext {
			continue
		}
		next.Components[name] = pc
	}
	mergeHandAuthored(prev, next)
	return kept
}

// mergeHandAuthored copies hand-authored fields (HealthyRaw,
// FailureModes, Vars, plus while-guards on matching deps) from prev
// onto next's components. Discovery only knows structural facts —
// type / resource / depends targets — so any semantic augmentation
// lives on prev and must survive rebuild.
func mergeHandAuthored(prev, next *model.Model) {
	for name, nextComp := range next.Components {
		prevComp, ok := prev.Components[name]
		if !ok {
			continue
		}
		if len(nextComp.HealthyRaw) == 0 && len(prevComp.HealthyRaw) > 0 {
			nextComp.HealthyRaw = append([]string(nil), prevComp.HealthyRaw...)
			nextComp.HealthyMode = prevComp.HealthyMode
			nextComp.Healthy = append([]expr.Node(nil), prevComp.Healthy...)
		}
		if len(nextComp.FailureModes) == 0 && len(prevComp.FailureModes) > 0 {
			nextComp.FailureModes = make(map[string][]string, len(prevComp.FailureModes))
			for k, v := range prevComp.FailureModes {
				nextComp.FailureModes[k] = append([]string(nil), v...)
			}
		}
		if len(nextComp.Vars) == 0 && len(prevComp.Vars) > 0 {
			nextComp.Vars = make(map[string]string, len(prevComp.Vars))
			for k, v := range prevComp.Vars {
				nextComp.Vars[k] = v
			}
		}
		// Port while-guards onto matching next-side deps (matched by
		// identical On target set). Discovery has no way to express
		// while-guards; if prev's dep matches next's by target, the
		// operator's guard applies to the merged edge.
		for i, nd := range nextComp.Depends {
			for _, pd := range prevComp.Depends {
				if pd.WhileRaw == "" || !sameTargets(nd.On, pd.On) {
					continue
				}
				nextComp.Depends[i].WhileRaw = pd.WhileRaw
				nextComp.Depends[i].While = pd.While
				break
			}
		}
	}
}

// sameTargets reports whether two depends-on lists target the same
// components (order-insensitive, duplicates collapsed).
func sameTargets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]struct{}, len(a))
	for _, s := range a {
		seen[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := seen[s]; !ok {
			return false
		}
	}
	return true
}

// Dangling lists, as "a depends on b", each authored component's
// dependency on a component m no longer has.
func Dangling(m *model.Model) []string {
	names := make([]string, 0, len(m.Components))
	for n := range m.Components {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []string
	for _, name := range names {
		c := m.Components[name]
		if !c.Authored() {
			continue
		}
		for _, dep := range c.Depends {
			for _, on := range dep.On {
				if m.Components[on] == nil {
					out = append(out, name+" depends on "+on)
				}
			}
		}
	}
	return out
}
