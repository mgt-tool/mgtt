// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"sort"

	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// eliminatesNamed is how many scenario IDs a probe over the graph names.
const eliminatesNamed = 10

// occamOnGraph is occam's decision without listing the chains: liveness
// per node and edge, counts by DP over the live graph, and only the
// shortest live layer built, since chain length is occam's first ranking
// key. It decides exactly as the listing does; false when the graph has a
// cycle and cannot be counted.
func occamOnGraph(in Input) (Decision, bool) {
	live := graphLiveness(in)
	counts, err := scenarios.Count(in.Graph, live)
	if err != nil {
		return Decision{}, false
	}
	if counts.Live == 0 {
		return Decision{Stuck: true, Reason: "no scenario matches observed facts"}, true
	}
	rank := func(layer []scenarios.Scenario) {
		type ranked struct {
			s       scenarios.Scenario
			suspect bool
			elim    int
		}
		rs := make([]ranked, len(layer))
		for i, s := range layer {
			rs[i] = ranked{s: s, suspect: touchesAnySuspect(s, in.Suspects)}
			if p := pickSymptomInward(s, in.Store, in.Model, in.Registry); p != nil {
				rs[i].elim = counts.Touch[p.Component] - 1
			}
		}
		sort.SliceStable(rs, func(i, j int) bool {
			if rs[i].suspect != rs[j].suspect {
				return rs[i].suspect
			}
			if rs[i].elim != rs[j].elim {
				return rs[i].elim > rs[j].elim
			}
			return scenarios.Less(rs[i].s, rs[j].s)
		})
		for i := range rs {
			layer[i] = rs[i].s
		}
	}
	var chosen scenarios.Scenario
	scenarios.LiveByLength(in.Graph, live, func(layer []scenarios.Scenario) bool {
		rank(layer)
		chosen = layer[0]
		return false
	})
	if counts.Live == 1 {
		return Decision{Done: true, RootCause: &chosen, Reason: "single scenario remains"}, true
	}
	probe := pickSymptomInward(chosen, in.Store, in.Model, in.Registry)
	if probe == nil {
		return Decision{Stuck: true, Reason: "chosen scenario fully verified but live set has multiple"}, true
	}
	probe.EliminatesCount = counts.Touch[probe.Component]
	scenarios.LiveByLength(in.Graph, live, func(layer []scenarios.Scenario) bool {
		rank(layer)
		for _, s := range layer {
			if s.TouchesComponent(probe.Component) {
				probe.Eliminates = append(probe.Eliminates, s.ID)
				if len(probe.Eliminates) == eliminatesNamed {
					return false
				}
			}
		}
		return true
	})
	return Decision{Probe: probe}, true
}

// graphLiveness is FilterLive's test, per node and per edge: a step the
// facts contradict, or a failure passing through a redundancy group that
// still holds, is dead. Each is decided once per decision.
func graphLiveness(in Input) scenarios.Liveness {
	nodes, edges := map[[2]string]bool{}, map[[2]string]bool{}
	return scenarios.Liveness{
		Node: func(c, s string) bool {
			k := [2]string{c, s}
			v, ok := nodes[k]
			if !ok {
				v = stepConsistent(scenarios.Step{Component: c, State: s}, in.Store, in.Model, in.Registry)
				nodes[k] = v
			}
			return v
		},
		Edge: func(a, b string) bool {
			if in.Model == nil || in.Store == nil {
				return true
			}
			k := [2]string{a, b}
			v, ok := edges[k]
			if !ok {
				held, _ := SatisfiedGroup(in.Model, in.Registry, in.Store, b, a)
				v = !held
				edges[k] = v
			}
			return v
		},
	}
}
