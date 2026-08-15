// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package facts_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/facts"
)

func TestAppendAndLatest(t *testing.T) {
	s := facts.NewInMemory()
	f := facts.Fact{Key: "ready_replicas", Value: 3, At: time.Now()}
	s.Append("api", f)

	got := s.Latest("api", "ready_replicas")
	if got == nil {
		t.Fatal("expected fact, got nil")
	}
	if got.Key != "ready_replicas" {
		t.Errorf("key: got %q want %q", got.Key, "ready_replicas")
	}
	if got.Value != 3 {
		t.Errorf("value: got %v want 3", got.Value)
	}
}

func TestLatestMissing(t *testing.T) {
	s := facts.NewInMemory()
	got := s.Latest("api", "ready_replicas")
	if got != nil {
		t.Errorf("expected nil for missing fact, got %+v", got)
	}
}

func TestLatestMissingComponent(t *testing.T) {
	s := facts.NewInMemory()
	s.Append("api", facts.Fact{Key: "ready_replicas", Value: 1})
	got := s.Latest("db", "ready_replicas")
	if got != nil {
		t.Errorf("expected nil for missing component, got %+v", got)
	}
}

func TestLatestReturnsNewest(t *testing.T) {
	s := facts.NewInMemory()
	t1 := time.Now().Add(-2 * time.Second)
	t2 := time.Now().Add(-1 * time.Second)
	t3 := time.Now()

	s.Append("api", facts.Fact{Key: "ready_replicas", Value: 1, At: t1})
	s.Append("api", facts.Fact{Key: "ready_replicas", Value: 2, At: t2})
	s.Append("api", facts.Fact{Key: "ready_replicas", Value: 3, At: t3})

	got := s.Latest("api", "ready_replicas")
	if got == nil {
		t.Fatal("expected fact, got nil")
	}
	if got.Value != 3 {
		t.Errorf("expected latest value 3, got %v", got.Value)
	}
}

func TestLatestDifferentKeys(t *testing.T) {
	s := facts.NewInMemory()
	s.Append("api", facts.Fact{Key: "ready_replicas", Value: 3})
	s.Append("api", facts.Fact{Key: "desired_replicas", Value: 5})

	r := s.Latest("api", "ready_replicas")
	if r == nil || r.Value != 3 {
		t.Errorf("ready_replicas: got %v", r)
	}
	d := s.Latest("api", "desired_replicas")
	if d == nil || d.Value != 5 {
		t.Errorf("desired_replicas: got %v", d)
	}
}

func TestFactsFor(t *testing.T) {
	s := facts.NewInMemory()
	s.Append("api", facts.Fact{Key: "a", Value: 1})
	s.Append("api", facts.Fact{Key: "b", Value: 2})
	s.Append("db", facts.Fact{Key: "c", Value: 3})

	apiF := s.FactsFor("api")
	if len(apiF) != 2 {
		t.Errorf("expected 2 facts for api, got %d", len(apiF))
	}
	dbF := s.FactsFor("db")
	if len(dbF) != 1 {
		t.Errorf("expected 1 fact for db, got %d", len(dbF))
	}
	noneF := s.FactsFor("missing")
	if len(noneF) != 0 {
		t.Errorf("expected 0 facts for missing, got %d", len(noneF))
	}
}

func TestAllComponents(t *testing.T) {
	s := facts.NewInMemory()
	s.Append("api", facts.Fact{Key: "a", Value: 1})
	s.Append("db", facts.Fact{Key: "b", Value: 2})
	s.Append("cache", facts.Fact{Key: "c", Value: 3})

	comps := s.AllComponents()
	if len(comps) != 3 {
		t.Errorf("expected 3 components, got %d: %v", len(comps), comps)
	}
	compSet := make(map[string]bool)
	for _, c := range comps {
		compSet[c] = true
	}
	for _, want := range []string{"api", "db", "cache"} {
		if !compSet[want] {
			t.Errorf("missing component %q in AllComponents", want)
		}
	}
}

func TestAllComponentsEmpty(t *testing.T) {
	s := facts.NewInMemory()
	comps := s.AllComponents()
	if len(comps) != 0 {
		t.Errorf("expected empty, got %v", comps)
	}
}

// TestIsAbsent_EmptyStore covers the "no facts at all" branch.
func TestIsAbsent_Empty(t *testing.T) {
	s := facts.NewInMemory()
	if s.IsAbsent("api") {
		t.Errorf("empty store: IsAbsent must be false")
	}
}

// TestIsAbsent_NormalFactsOnly verifies that facts with the default
// empty Status don't trigger IsAbsent.
func TestIsAbsent_NormalFactsOnly(t *testing.T) {
	s := facts.NewInMemory()
	s.Append("api", facts.Fact{Key: "ready_replicas", Value: 3})
	if s.IsAbsent("api") {
		t.Errorf("component with only normal facts: IsAbsent must be false")
	}
}

// TestIsAbsent_AllFactsNotFound verifies a component whose probes all
// returned not_found (no resolved value) reads as absent.
func TestIsAbsent_AllFactsNotFound(t *testing.T) {
	s := facts.NewInMemory()
	s.Append("api", facts.Fact{Key: "exists", Status: facts.FactStatusNotFound})
	s.Append("api", facts.Fact{Key: "ready_replicas", Status: facts.FactStatusNotFound})
	if !s.IsAbsent("api") {
		t.Errorf("component with only not_found facts: IsAbsent must be true")
	}
	// Unrelated component still reads normal.
	if s.IsAbsent("db") {
		t.Errorf("unrelated component must not be flagged absent")
	}
}

// TestIsAbsent_ResolvedValueMasksNotFound is the regression guard for the
// masking bug: a not_found on one sub-resource fact must NOT mark the
// component absent when a sibling fact resolved to a real value. The
// component clearly exists (and may be broken) — treating it as absent
// would drop the scenario that explains the failure.
func TestIsAbsent_ResolvedValueMasksNotFound(t *testing.T) {
	s := facts.NewInMemory()
	s.Append("api", facts.Fact{Key: "ready_replicas", Value: 0}) // resolved: broken
	s.Append("api", facts.Fact{Key: "endpoints", Status: facts.FactStatusNotFound})
	if s.IsAbsent("api") {
		t.Errorf("component with a resolved value must NOT read as absent despite a not_found sibling")
	}
}

// TestPartialVisibility counts forbidden/transient facts authoritatively
// from Fact.Status (deduped per key, latest write wins) — the signal the
// diagnose partial-visibility warning now reads instead of parsing
// outcome strings.
func TestPartialVisibility(t *testing.T) {
	s := facts.NewInMemory()
	if f, tr := s.PartialVisibility(); f != 0 || tr != 0 {
		t.Errorf("empty store: got forbidden=%d transient=%d, want 0/0", f, tr)
	}

	s.Append("api", facts.Fact{Key: "ready_replicas", Value: 3})                       // resolved
	s.Append("api", facts.Fact{Key: "crd", Status: facts.FactStatusForbidden})         // forbidden
	s.Append("store", facts.Fact{Key: "available", Status: facts.FactStatusTransient}) // transient
	s.Append("edge", facts.Fact{Key: "upstream", Status: facts.FactStatusForbidden})   // forbidden
	s.Append("edge", facts.Fact{Key: "upstream", Value: 4})                            // later resolved → not counted

	forbidden, transient := s.PartialVisibility()
	if forbidden != 1 {
		t.Errorf("forbidden = %d, want 1 (api.crd; edge.upstream resolved later)", forbidden)
	}
	if transient != 1 {
		t.Errorf("transient = %d, want 1 (store.available)", transient)
	}
}

func TestAppendAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.state.yaml")
	meta := facts.StoreMeta{Model: "test", Version: "1.0", Incident: "inc-001", Started: time.Now()}
	s := facts.NewDiskBacked(path, meta)

	err := s.AppendAndSave("store", facts.Fact{Key: "available", Value: true, Collector: "datalayer", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("state file not created")
	}
}
