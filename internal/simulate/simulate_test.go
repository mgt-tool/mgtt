package simulate

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func loadStorefront(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()
	root := repoRoot()
	m, err := model.Load(filepath.Join(root, "testdata", "models", "sample-model.yaml"))
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	reg := providersupport.NewRegistry()
	for _, pair := range []struct{ name, file string }{
		{"kubernetes", "testdata/kubernetes-provider.yaml"},
		{"aws", "testdata/aws-provider.yaml"},
	} {
		p, err := providersupport.LoadFromFile(pair.file)
		if err != nil {
			t.Fatalf("load provider %s: %v", pair.name, err)
		}
		reg.Register(p)
	}
	return m, reg
}

// TestLoadScenario verifies the scenario YAML loader's type normalization —
// ints stay int, bools stay bool, strings stay string. Uses a synthetic
// scenario, not a real-world one.
func TestLoadScenario(t *testing.T) {
	sc, err := LoadScenario("testdata/scenarios/normalization.yaml")
	if err != nil {
		t.Fatalf("load scenario: %v", err)
	}

	if sc.Name != "loader normalization" {
		t.Errorf("name = %q, want %q", sc.Name, "loader normalization")
	}

	if v, ok := sc.Inject["comp_a"]["restart_count"]; ok {
		if _, isInt := v.(int); !isInt {
			t.Errorf("inject.comp_a.restart_count is %T, want int", v)
		}
	} else {
		t.Error("inject.comp_a.restart_count missing")
	}

	if v, ok := sc.Inject["comp_a"]["ready"]; ok {
		if _, isBool := v.(bool); !isBool {
			t.Errorf("inject.comp_a.ready is %T, want bool", v)
		}
	} else {
		t.Error("inject.comp_a.ready missing")
	}

	if v, ok := sc.Inject["comp_a"]["phase"]; ok {
		if _, isString := v.(string); !isString {
			t.Errorf("inject.comp_a.phase is %T, want string", v)
		}
	} else {
		t.Error("inject.comp_a.phase missing")
	}
}
