// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// legacyOccamSuggest is Occam's SuggestProbe as it stood before the
// cross-elimination count was computed in one pass: the count is
// recomputed inside the sort comparator, O(n² log n). It is kept here as
// the oracle the faster version must agree with decision for decision.
func legacyOccamSuggest(in Input) Decision {
	live := FilterLive(in.Scenarios, in.Store, in.Model, in.Registry)
	switch len(live) {
	case 0:
		return Decision{Stuck: true, Reason: "no scenario matches observed facts"}
	case 1:
		return Decision{Done: true, RootCause: &live[0], Reason: "single scenario remains"}
	}
	sort.SliceStable(live, func(i, j int) bool {
		if live[i].Length() != live[j].Length() {
			return live[i].Length() < live[j].Length()
		}
		si := touchesAnySuspect(live[i], in.Suspects)
		sj := touchesAnySuspect(live[j], in.Suspects)
		if si != sj {
			return si
		}
		ei := legacyCrossEliminationCount(live[i], live, in.Store, in.Model, in.Registry)
		ej := legacyCrossEliminationCount(live[j], live, in.Store, in.Model, in.Registry)
		if ei != ej {
			return ei > ej
		}
		return scenarios.Less(live[i], live[j])
	})
	chosen := live[0]
	probe := pickSymptomInward(chosen, in.Store, in.Model, in.Registry)
	if probe == nil {
		return Decision{Stuck: true, Reason: "chosen scenario fully verified but live set has multiple"}
	}
	for _, s := range live {
		for _, step := range s.Chain {
			if step.Component == probe.Component {
				probe.Eliminates = append(probe.Eliminates, s.ID)
				break
			}
		}
	}
	return Decision{Probe: probe}
}

func legacyCrossEliminationCount(s scenarios.Scenario, live []scenarios.Scenario, store *facts.Store, m *model.Model, reg *providersupport.Registry) int {
	probe := pickSymptomInward(s, store, m, reg)
	if probe == nil {
		return 0
	}
	n := 0
	for _, other := range live {
		if other.ID == s.ID {
			continue
		}
		for _, step := range other.Chain {
			if step.Component == probe.Component {
				n++
				break
			}
		}
	}
	return n
}

// layeredModel builds edge → 3 web → 3 api → 2 store, each tier fanning
// out to two of the next, from the testdata provider types. Enough chains
// that ties on length, suspects and elimination count all occur.
func layeredModel(t testing.TB) (*model.Model, *providersupport.Registry) {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	var b strings.Builder
	b.WriteString("meta:\n  name: layered\n  version: \"1.0\"\n  providers: [compute, datalayer]\ncomponents:\n")
	b.WriteString("  edge:\n    type: gateway\n    depends:\n      - on: web-a\n      - on: web-b\n      - on: web-c\n")
	tiers := []struct{ name, typ, next string }{{"web", "workload", "api"}, {"api", "workload", "store"}}
	for _, tier := range tiers {
		for i, x := range []string{"a", "b", "c"} {
			n1, n2 := []string{"a", "b", "c"}[i%3], []string{"b", "c", "a"}[i%3]
			if tier.next == "store" {
				n1, n2 = "a", "b"
			}
			fmt.Fprintf(&b, "  %s-%s:\n    type: %s\n    depends:\n      - on: %s-%s\n      - on: %s-%s\n", tier.name, x, tier.typ, tier.next, n1, tier.next, n2)
		}
	}
	for _, x := range []string{"a", "b"} {
		fmt.Fprintf(&b, "  store-%s:\n    providers: [datalayer]\n    type: datastore\n", x)
	}
	path := filepath.Join(t.TempDir(), "system.model.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := model.Load(path)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	reg := providersupport.NewRegistry()
	for _, f := range []string{"compute.yaml", "datalayer.yaml"} {
		p, err := providersupport.LoadFromFile(filepath.Join(root, "testdata", "providers", f))
		if err != nil {
			t.Fatal(err)
		}
		reg.Register(p)
	}
	return m, reg
}

// randomStore observes a random subset of components with random values,
// and sometimes records a fact as unreadable instead.
func randomStore(rng *rand.Rand, m *model.Model) *facts.Store {
	values := map[string]func() any{
		"upstream_count":   func() any { return rng.IntN(4) },
		"ready_replicas":   func() any { return rng.IntN(4) },
		"desired_replicas": func() any { return 3 },
		"restart_count":    func() any { return rng.IntN(10) },
		"endpoints":        func() any { return rng.IntN(4) },
		"available":        func() any { return rng.IntN(2) == 0 },
		"connection_count": func() any { return rng.IntN(600) },
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	store := facts.NewInMemory()
	for _, comp := range m.Order {
		if rng.IntN(3) != 0 {
			continue
		}
		for _, k := range keys {
			switch rng.IntN(4) {
			case 0:
				store.Append(comp, facts.Fact{Key: k, Value: values[k](), At: time.Now()})
			case 1:
				store.Append(comp, facts.Fact{Key: k, Status: facts.FactStatusForbidden, At: time.Now()})
			}
		}
	}
	return store
}

// The linear occam must reach the same decision as the quadratic one —
// same probe, same Eliminates order, same Done/Stuck and root cause — on
// every store.
func TestOccam_MatchesLegacyDecisions(t *testing.T) {
	m, reg := layeredModel(t)
	scs := scenarios.Enumerate(m, reg)
	if len(scs) < 300 { // 352 unique chains; duplicates once made it 500+
		t.Fatalf("layered model enumerates %d scenarios; want enough for ties", len(scs))
	}
	rng := rand.New(rand.NewPCG(1, 2))
	suspects := []SuspectHint{{Component: "api-b"}, {Component: "store-a", State: "stopped"}}
	kinds := map[string]int{}
	for i := 0; i < 300; i++ {
		in := Input{Model: m, Registry: reg, Store: randomStore(rng, m), Scenarios: scs}
		if rng.IntN(3) == 0 {
			in.Suspects = suspects[:1+rng.IntN(2)]
		}
		want := legacyOccamSuggest(in)
		got := Occam().SuggestProbe(in)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("store %d: decisions differ\n got: %+v\nwant: %+v", i, describe(got), describe(want))
		}
		switch {
		case got.Probe != nil:
			kinds["probe"]++
		case got.Done:
			kinds["done"]++
		default:
			kinds["stuck"]++
		}
	}
	t.Logf("%d scenarios; decisions: %v", len(scs), kinds)
	if kinds["probe"] < 100 {
		t.Fatalf("too few probe decisions to exercise the ranking: %v", kinds)
	}
}

func describe(d Decision) string {
	s := fmt.Sprintf("done=%v stuck=%v reason=%q", d.Done, d.Stuck, d.Reason)
	if d.Probe != nil {
		s += fmt.Sprintf(" probe=%s.%s eliminates=%d", d.Probe.Component, d.Probe.Fact, len(d.Probe.Eliminates))
	}
	if d.RootCause != nil {
		s += " root=" + d.RootCause.ID
	}
	return s
}
