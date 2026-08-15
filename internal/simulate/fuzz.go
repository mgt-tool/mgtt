// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"fmt"
	"math/rand/v2"

	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// Fuzz runs `iterations` random scenario-truncation checks. Each pick:
//  1. Choose a scenario uniformly at random.
//  2. Truncate its chain to a random prefix length 0..len(chain)-1.
//  3. Synthesize facts for that prefix into an in-memory store.
//  4. Ask Occam for a suggestion. Any non-Stuck outcome counts as pass
//     when truncation > 0 (the strategy can either converge on the root
//     or continue probing; both are valid). With an empty store
//     (truncation == 0) Occam should not already be Stuck — that would
//     mean the scenario set contradicts itself — so it still counts as
//     fail.
//
// Returns (passed, failed, details).
func Fuzz(m *model.Model, reg *providersupport.Registry, scs []scenarios.Scenario, iterations int, seed int64) (passed, failed int, details []string) {
	if len(scs) == 0 {
		return 0, 0, []string{"no scenarios to fuzz"}
	}
	// PCG (v2) is a deterministic non-crypto PRNG. Seed → reproducible
	// run order; crypto randomness would defeat the point of --fuzz-seed.
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed>>32)))
	for i := 0; i < iterations; i++ {
		s := scs[rng.IntN(len(scs))]
		maxTrunc := len(s.Chain)
		truncation := 0
		if maxTrunc > 0 {
			truncation = rng.IntN(maxTrunc + 1)
		}
		store := facts.NewInMemory()
		if err := synthesizeFactsForSteps(store, m, reg, s.Chain[:truncation]); err != nil {
			failed++
			details = append(details, fmt.Sprintf("fuzz #%d %s[:%d]: FAIL synth — %v", i+1, s.ID, truncation, err))
			continue
		}
		in := strategy.Input{Model: m, Registry: reg, Store: store, Scenarios: scs}
		d := strategy.Occam().SuggestProbe(in)
		if d.Stuck {
			failed++
			details = append(details, fmt.Sprintf("fuzz #%d %s[:%d]: FAIL Stuck — %s", i+1, s.ID, truncation, d.Reason))
			continue
		}
		passed++
		details = append(details, fmt.Sprintf("fuzz #%d %s[:%d]: PASS", i+1, s.ID, truncation))
	}
	return
}
