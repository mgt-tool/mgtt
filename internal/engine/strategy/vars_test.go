package strategy

import (
	"reflect"
	"testing"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// BFS must carry the merged var map on the Probe — component-level
// Vars override meta.vars on key collision, and keys not present in
// the component block fall through unchanged. Regression coverage for
// the "ESO lives in external-secrets namespace but model-wide
// namespace=default" bug.
func TestBFS_ProbeCarriesMergedVars(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{
			Providers: []string{"p"},
			Vars: map[string]string{
				"namespace": "default",
				"region":    "eu-central-1",
			},
		},
		Components: map[string]*model.Component{
			"eso": {
				Name: "eso",
				Type: "operator",
				Vars: map[string]string{
					"namespace": "external-secrets", // override for this component only
				},
			},
		},
		Order: []string{"eso"},
	}
	m.BuildGraph()
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta: providersupport.ProviderMeta{Name: "p"},
		Types: map[string]*providersupport.Type{
			"operator": {
				Name: "operator",
				Facts: map[string]*providersupport.FactSpec{
					"deployment_ready": {Probe: providersupport.ProbeDef{Cmd: "x", Cost: "low", Access: "read"}},
				},
			},
		},
	})
	store := facts.NewInMemory()

	dec := BFS().SuggestProbe(Input{Model: m, Registry: reg, Store: store})
	if dec.Probe == nil {
		t.Fatalf("want a probe; got %+v", dec)
	}
	want := map[string]string{
		"namespace": "external-secrets", // component override wins
		"region":    "eu-central-1",     // inherited from meta
	}
	if !reflect.DeepEqual(dec.Probe.Vars, want) {
		t.Errorf("Probe.Vars = %v, want %v", dec.Probe.Vars, want)
	}
}

// mergeVars helper contract: empty inputs → nil, collision → override wins.
func TestMergeVars(t *testing.T) {
	if got := mergeVars(nil, nil); got != nil {
		t.Errorf("mergeVars(nil, nil) = %v, want nil", got)
	}
	if got := mergeVars(nil, map[string]string{"a": "1"}); !reflect.DeepEqual(got, map[string]string{"a": "1"}) {
		t.Errorf("mergeVars(nil, {a:1}) = %v, want {a:1}", got)
	}
	got := mergeVars(
		map[string]string{"a": "base", "b": "base"},
		map[string]string{"a": "override", "c": "new"},
	)
	want := map[string]string{"a": "override", "b": "base", "c": "new"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeVars = %v, want %v", got, want)
	}
}

// silence unused-import linter if a helper isn't referenced yet.
var _ = expr.CmpNode{}
