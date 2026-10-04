// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// A type says what healthy means twice: in its healthy rules, and in which
// of its states is the default. When the two disagree for some facts,
// simulate (which reads states) and diagnose (which reads rules) disagree
// too. HealthStateDisagreements searches for facts where they do.

// Disagreement is a set of facts on which healthy rules and the type's
// states part company.
type Disagreement struct {
	State   string         // the state those facts put the component in
	Healthy bool           // what the healthy rules say
	Witness map[string]any // the facts
}

// String renders the witness facts, sorted.
func (d Disagreement) WitnessString() string {
	keys := make([]string, 0, len(d.Witness))
	for k := range d.Witness {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%v", k, d.Witness[k])
	}
	return strings.Join(parts, ", ")
}

// maxAssignments bounds the search; beyond it, assignments are sampled.
const maxAssignments = 4096

// HealthStateDisagreements tries fact values around every constant the
// rules and states compare against, and returns, for each state, one set
// of facts where the rules call the component healthy outside the
// default state, or unhealthy in it. Assignments where a rule or every
// state is undecided are skipped. vars resolves per-component thresholds;
// it may be nil.
func HealthStateDisagreements(rules []expr.Node, t *providersupport.Type, component string, vars expr.VarLookup) []Disagreement {
	if t == nil || t.DefaultActiveState == "" || len(rules) == 0 {
		return nil
	}
	cands := candidates(rules, t, component, vars)
	names := make([]string, 0, len(cands))
	for n := range cands {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil
	}

	total := 1
	for _, n := range names {
		total *= len(cands[n])
		if total > maxAssignments {
			break
		}
	}
	rng := rand.New(rand.NewPCG(1, uint64(len(names))))
	seen := map[string]bool{}
	var out []Disagreement
	for i := 0; i < maxAssignments && (total > maxAssignments || i < total); i++ {
		assign := map[string]any{}
		idx := i
		for _, n := range names {
			vals := cands[n]
			if total > maxAssignments {
				assign[n] = vals[rng.IntN(len(vals))]
			} else {
				assign[n] = vals[idx%len(vals)]
				idx /= len(vals)
			}
		}
		ctx := expr.Ctx{CurrentComponent: component, Facts: mapLookup{component: assign}, Vars: vars}
		healthy, decided := allTrue(rules, ctx)
		if !decided {
			continue
		}
		state := ""
		for _, st := range t.States {
			if st.When == nil {
				continue
			}
			if ok, err := st.When.Eval(ctx); err == nil && ok {
				state = st.Name
				break
			}
		}
		if state == "" || healthy == (state == t.DefaultActiveState) {
			continue
		}
		key := fmt.Sprintf("%s/%v", state, healthy)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Disagreement{State: state, Healthy: healthy, Witness: assign})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].State != out[j].State {
			return out[i].State < out[j].State
		}
		return out[i].Healthy && !out[j].Healthy
	})
	return out
}

func allTrue(rules []expr.Node, ctx expr.Ctx) (healthy, decided bool) {
	healthy = true
	for _, r := range rules {
		ok, err := r.Eval(ctx)
		if err != nil {
			return false, false
		}
		healthy = healthy && ok
	}
	return healthy, true
}

// candidates collects, per fact the rules and states read, the values
// worth trying: both booleans; the string literals compared with, and one
// other; and each number compared with, with its neighbours.
func candidates(rules []expr.Node, t *providersupport.Type, component string, vars expr.VarLookup) map[string][]any {
	nums := map[string]map[float64]bool{}
	strs := map[string]map[string]bool{}
	addNum := func(fact string, v float64) {
		if nums[fact] == nil {
			nums[fact] = map[float64]bool{}
		}
		nums[fact][v] = true
	}
	var walk func(expr.Node)
	walk = func(n expr.Node) {
		switch v := n.(type) {
		case *expr.AndNode:
			walk(v.L)
			walk(v.R)
		case *expr.OrNode:
			walk(v.L)
			walk(v.R)
		case *expr.CmpNode:
			f := t.Facts[v.Fact]
			if v.Component != "" || f == nil {
				return
			}
			switch val := v.Value.(type) {
			case int:
				addNum(v.Fact, float64(val))
			case float64:
				addNum(v.Fact, val)
			case string:
				if f.TypeName == "mgtt.string" {
					if strs[v.Fact] == nil {
						strs[v.Fact] = map[string]bool{}
					}
					strs[v.Fact][val] = true
				} else if other := t.Facts[val]; other != nil {
					addNum(v.Fact, 0)
					addNum(val, 0)
					// Compared with another fact: let them meet.
					nums[val][1], nums[v.Fact][1] = true, true
				} else if vars != nil {
					if raw, ok := vars.LookupVar(component, val); ok {
						if x, err := strconv.ParseFloat(raw, 64); err == nil {
							addNum(v.Fact, x)
						}
					}
				}
			}
		}
	}
	for _, r := range rules {
		walk(r)
	}
	for _, st := range t.States {
		if st.When != nil {
			walk(st.When)
		}
	}

	out := map[string][]any{}
	for name, f := range t.Facts {
		switch {
		case f.TypeName == "mgtt.bool":
			out[name] = []any{true, false}
		case f.TypeName == "mgtt.string" && strs[name] != nil:
			var vals []any
			for s := range strs[name] {
				vals = append(vals, s)
			}
			sort.Slice(vals, func(i, j int) bool { return vals[i].(string) < vals[j].(string) })
			out[name] = append(vals, "<other>")
		case nums[name] != nil:
			isInt := f.TypeName == "mgtt.int"
			set := map[float64]bool{}
			for c := range nums[name] {
				step := 1.0
				if !isInt {
					step = 0.001
				}
				for _, x := range []float64{c - step, c, c + step} {
					if x >= 0 {
						set[x] = true
					}
				}
			}
			var vals []float64
			for x := range set {
				vals = append(vals, x)
			}
			sort.Float64s(vals)
			for _, x := range vals {
				if isInt {
					out[name] = append(out[name], int(x))
				} else {
					out[name] = append(out[name], x)
				}
			}
		}
	}
	return out
}

type mapLookup map[string]map[string]any

func (m mapLookup) LookupValue(component, key string) (any, bool) {
	v, ok := m[component][key]
	return v, ok
}
