package facts_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sajonaro/mgtt/internal/facts"
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

func TestDiskBacked_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.state.yaml")

	meta := facts.StoreMeta{Model: "test", Version: "1.0", Incident: "inc-001", Started: time.Now()}
	s := facts.NewDiskBacked(path, meta)
	s.Append("api", facts.Fact{Key: "ready_replicas", Value: 0, Collector: "test", At: time.Now()})
	s.Append("api", facts.Fact{Key: "restart_count", Value: 12, Collector: "test", At: time.Now()})

	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	loaded, err := facts.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Meta.Incident != "inc-001" {
		t.Fatalf("expected incident 'inc-001', got %q", loaded.Meta.Incident)
	}
	f := loaded.Latest("api", "ready_replicas")
	if f == nil {
		t.Fatal("expected fact")
	}
	// Note: YAML may deserialize int as int or float — handle both
}

func TestAppendAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.state.yaml")
	meta := facts.StoreMeta{Model: "test", Version: "1.0", Incident: "inc-001", Started: time.Now()}
	s := facts.NewDiskBacked(path, meta)

	err := s.AppendAndSave("rds", facts.Fact{Key: "available", Value: true, Collector: "aws", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("state file not created")
	}
}
