// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// layeredSynth builds an entry gateway over `layers` tiers of `width`
// components, each depending on `fan` components of the tier below,
// ending in datastores: the shape whose chain count grows with depth.
func layeredSynth(tb testing.TB, layers, width, fan int) (*model.Model, *providersupport.Registry) {
	tb.Helper()
	_, reg := layeredModel(tb)
	var b strings.Builder
	b.WriteString("meta:\n  name: synth\n  version: \"1.0\"\n  providers: [compute, datalayer]\ncomponents:\n  edge:\n    type: gateway\n    depends:\n")
	for j := 0; j < width; j++ {
		fmt.Fprintf(&b, "      - on: c0-%d\n", j)
	}
	for l := 0; l < layers; l++ {
		for j := 0; j < width; j++ {
			if l == layers-1 {
				fmt.Fprintf(&b, "  c%d-%d:\n    providers: [datalayer]\n    type: datastore\n", l, j)
				continue
			}
			fmt.Fprintf(&b, "  c%d-%d:\n    type: workload\n    depends:\n", l, j)
			for f := 0; f < fan; f++ {
				fmt.Fprintf(&b, "      - on: c%d-%d\n", l+1, (j+f)%width)
			}
		}
	}
	path := filepath.Join(tb.TempDir(), "system.model.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		tb.Fatal(err)
	}
	m, err := model.Load(path)
	if err != nil {
		tb.Fatal(err)
	}
	return m, reg
}

// Chain count grows about 6x per tier at width 10, fan-out 2: depth, not
// size, is what listing chains pays for. Counting and deciding over the
// graph do not list them. Measured 2026-10-08 (4 cores):
//
//	layers  chains       list   count  decision (list)  decision (graph)
//	4       7,471        55ms   4ms    23ms             7ms
//	5       45,451       0.4s   9ms    0.2s             14ms
//	6       273,511      3.2s   18ms   0.9s             27ms
//	7       1,642,051    -      23ms   -                46ms
//	8       9,853,471    -      38ms   -                70ms
//	10      354,734,551  -      73ms   -                132ms
//
// Listing stops at 6 layers for the benchmark's run time.
//
// go test ./internal/engine/strategy -run '^$' -bench Scale -benchtime 1x
func BenchmarkScale(b *testing.B) {
	for _, layers := range []int{4, 5, 6} {
		m, reg := layeredSynth(b, layers, 10, 2)
		g := scenarios.BuildGraph(m, reg)
		var scs []scenarios.Scenario
		b.Run(fmt.Sprintf("expand/layers=%d", layers), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				scs = scenarios.Expand(g)
			}
			b.ReportMetric(float64(len(scs)), "chains")
		})
		b.Run(fmt.Sprintf("decision/layers=%d", layers), func(b *testing.B) {
			in := Input{Model: m, Registry: reg, Store: facts.NewInMemory(), Scenarios: scs}
			for i := 0; i < b.N; i++ {
				Occam().SuggestProbe(in)
			}
		})
	}
	// Counting over the graph needs no chains at all, so it goes deeper
	// than listing can.
	for _, layers := range []int{4, 5, 6, 7, 8, 10} {
		m, reg := layeredSynth(b, layers, 10, 2)
		g := scenarios.BuildGraph(m, reg)
		b.Run(fmt.Sprintf("decision-graph/layers=%d", layers), func(b *testing.B) {
			in := Input{Model: m, Registry: reg, Store: facts.NewInMemory(), Graph: g}
			for i := 0; i < b.N; i++ {
				Occam().SuggestProbe(in)
			}
		})
		// What --write-scenarios, validate and diff pay to count the chains.
		b.Run(fmt.Sprintf("count/layers=%d", layers), func(b *testing.B) {
			n := 0
			for i := 0; i < b.N; i++ {
				n = scenarios.CountChains(g)
			}
			b.ReportMetric(float64(n), "chains")
		})
	}
}
