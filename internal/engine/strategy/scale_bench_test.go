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
// size, is what scenario-guided diagnosis pays for, because every chain is
// materialised. Measured 2026-10-04 (4 cores):
//
//	layers  components  chains     expand  one decision
//	4       41          10,921     50ms    15ms
//	5       51          66,181     0.4s    128ms
//	6       61          397,921    2.7s    0.7s
//	7       71          2,388,541  20s     5.5s
//	8       81          did not finish in 60s
//
// Layers 7 and 8 are left out of the benchmark for its run time.
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
}
