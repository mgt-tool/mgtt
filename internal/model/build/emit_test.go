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
