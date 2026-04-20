package build

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/model"
)

// Emitting the same model twice MUST produce byte-identical output.
// This is the enforceable half of the spec invariant "generated
// artifacts are graph-equivalent run-to-run" — we go further and
// guarantee byte-equivalence as an implementation choice because
// git-diff reviewability matters.
func TestEmitYAML_Deterministic(t *testing.T) {
	m := makeFixtureModel()
	var out1, out2 bytes.Buffer
	if err := EmitYAML(m, &out1); err != nil {
		t.Fatal(err)
	}
	if err := EmitYAML(m, &out2); err != nil {
		t.Fatal(err)
	}
	if out1.String() != out2.String() {
		t.Errorf("emit non-deterministic:\nfirst:\n%s\nsecond:\n%s", out1.String(), out2.String())
	}
}

// Components emit in sorted order regardless of insertion order.
func TestEmitYAML_ComponentsSorted(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "fixture", Version: "1.0"},
		Components: map[string]*model.Component{
			"zeta":  {Name: "zeta", Type: "deployment"},
			"alpha": {Name: "alpha", Type: "deployment"},
			"mid":   {Name: "mid", Type: "deployment"},
		},
	}
	var out bytes.Buffer
	if err := EmitYAML(m, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	iAlpha := strings.Index(s, "alpha:")
	iMid := strings.Index(s, "mid:")
	iZeta := strings.Index(s, "zeta:")
	if !(iAlpha < iMid && iMid < iZeta) {
		t.Errorf("components not sorted (positions: alpha=%d mid=%d zeta=%d)\n%s", iAlpha, iMid, iZeta, s)
	}
}

// Dependencies within a component emit in sorted order.
func TestEmitYAML_DependenciesSorted(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "fixture", Version: "1.0"},
		Components: map[string]*model.Component{
			"api": {
				Name: "api",
				Type: "deployment",
				Depends: []model.Dependency{
					{On: []string{"redis"}},
					{On: []string{"rds"}},
					{On: []string{"mq"}},
				},
			},
		},
	}
	var out bytes.Buffer
	if err := EmitYAML(m, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	iMq := strings.Index(s, "mq")
	iRds := strings.Index(s, "rds")
	iRedis := strings.Index(s, "redis")
	if !(iMq < iRds && iRds < iRedis) {
		t.Errorf("deps not sorted (mq=%d rds=%d redis=%d)\n%s", iMq, iRds, iRedis, s)
	}
}

// If two models encode the same graph (same components, deps,
// providers, everything) but were built by inserting components in
// different orders into their underlying maps, the emitted YAML must
// still be byte-identical. This guards against future refactors that
// could accidentally pick up Go's map-iteration-order randomness as
// output order.
func TestEmitYAML_PermutationDeterminism(t *testing.T) {
	// Build two semantically-identical models by inserting components
	// in reverse orders.
	mA := &model.Model{
		Meta: model.Meta{Name: "fixture", Version: "1.0"},
		Components: map[string]*model.Component{},
	}
	names := []string{"alpha", "mid", "zeta"}
	for _, n := range names {
		mA.Components[n] = &model.Component{Name: n, Type: "deployment"}
	}
	mB := &model.Model{
		Meta: model.Meta{Name: "fixture", Version: "1.0"},
		Components: map[string]*model.Component{},
	}
	for i := len(names) - 1; i >= 0; i-- {
		n := names[i]
		mB.Components[n] = &model.Component{Name: n, Type: "deployment"}
	}

	var outA, outB bytes.Buffer
	if err := EmitYAML(mA, &outA); err != nil {
		t.Fatal(err)
	}
	if err := EmitYAML(mB, &outB); err != nil {
		t.Fatal(err)
	}
	if outA.String() != outB.String() {
		t.Errorf("permutation produced different output:\nA (insertion: alpha, mid, zeta):\n%s\nB (insertion: zeta, mid, alpha):\n%s", outA.String(), outB.String())
	}
}

func makeFixtureModel() *model.Model {
	return &model.Model{
		Meta: model.Meta{Name: "fixture", Version: "1.0", Providers: []string{"kubernetes", "aws"}},
		Components: map[string]*model.Component{
			"api": {
				Name:    "api",
				Type:    "deployment",
				Depends: []model.Dependency{{On: []string{"rds"}}},
			},
			"rds": {Name: "rds", Type: "rds_instance", Providers: []string{"aws"}},
		},
	}
}
