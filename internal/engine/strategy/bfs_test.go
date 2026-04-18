package strategy

import (
	"testing"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// tinyModel builds a two-component model with 'web' depending on 'db',
// each backed by a type with two facts ("a" and "b") from provider "p".
// Entry point is 'web' (nothing depends on it).
func tinyModel(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()
	webType := &providersupport.Type{
		Name: "web",
		Facts: map[string]*providersupport.FactSpec{
			"a": {Probe: providersupport.ProbeDef{Cmd: "web-a", Cost: "cheap", Access: "read"}},
			"b": {Probe: providersupport.ProbeDef{Cmd: "web-b", Cost: "cheap", Access: "read"}},
		},
	}
	dbType := &providersupport.Type{
		Name: "db",
		Facts: map[string]*providersupport.FactSpec{
			"status": {Probe: providersupport.ProbeDef{Cmd: "db-status", Cost: "cheap", Access: "read"}},
		},
	}
	prov := &providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{
			"web": webType,
			"db":  dbType,
		},
	}
	reg := providersupport.NewRegistry()
	reg.Register(prov)

	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"web": {
				Name: "web",
				Type: "web",
				Depends: []model.Dependency{
					{On: []string{"db"}},
				},
			},
			"db": {Name: "db", Type: "db"},
		},
		Order: []string{"web", "db"},
	}
	m.BuildGraph()
	return m, reg
}

func TestBFS_FirstProbeIsEntryAlphaFirst(t *testing.T) {
	m, reg := tinyModel(t)
	store := facts.NewInMemory()
	in := Input{Model: m, Registry: reg, Store: store}

	dec := BFS().SuggestProbe(in)
	if dec.Probe == nil {
		t.Fatalf("want a probe; got done=%v stuck=%v reason=%s", dec.Done, dec.Stuck, dec.Reason)
	}
	if dec.Probe.Component != "web" {
		t.Errorf("want probe on entry 'web'; got %q", dec.Probe.Component)
	}
	if dec.Probe.Fact != "a" {
		t.Errorf("want alphabetically-first fact 'a'; got %q", dec.Probe.Fact)
	}
}

func TestBFS_MovesToDepWhenEntryCovered(t *testing.T) {
	m, reg := tinyModel(t)
	store := facts.NewInMemory()
	// Pre-populate web's facts.
	store.Append("web", facts.Fact{Key: "a", Value: "x"})
	store.Append("web", facts.Fact{Key: "b", Value: "y"})

	dec := BFS().SuggestProbe(Input{Model: m, Registry: reg, Store: store})
	if dec.Probe == nil {
		t.Fatalf("want probe on db; got %+v", dec)
	}
	if dec.Probe.Component != "db" {
		t.Errorf("want probe on 'db'; got %q", dec.Probe.Component)
	}
}

func TestBFS_DoneWhenAllFactsCollected(t *testing.T) {
	m, reg := tinyModel(t)
	store := facts.NewInMemory()
	store.Append("web", facts.Fact{Key: "a", Value: "x"})
	store.Append("web", facts.Fact{Key: "b", Value: "y"})
	store.Append("db", facts.Fact{Key: "status", Value: "up"})

	dec := BFS().SuggestProbe(Input{Model: m, Registry: reg, Store: store})
	if dec.Probe != nil {
		t.Errorf("want no probe when facts exhausted; got %+v", dec.Probe)
	}
	if !dec.Done {
		t.Errorf("want Done=true; got %+v", dec)
	}
}

func TestBFS_Name(t *testing.T) {
	if BFS().Name() != "bfs" {
		t.Errorf("want bfs; got %s", BFS().Name())
	}
}
