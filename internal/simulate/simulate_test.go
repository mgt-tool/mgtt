package simulate

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sajonaro/mgtt/internal/model"
	"github.com/sajonaro/mgtt/internal/providersupport"
)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func loadStorefront(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()
	root := repoRoot()
	m, err := model.Load(filepath.Join(root, "examples", "storefront", "system.model.yaml"))
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

func TestLoadScenario(t *testing.T) {
	root := repoRoot()
	sc, err := LoadScenario(filepath.Join(root, "scenarios", "rds-unavailable.yaml"))
	if err != nil {
		t.Fatalf("load scenario: %v", err)
	}

	if sc.Name != "rds unavailable" {
		t.Errorf("name = %q, want %q", sc.Name, "rds unavailable")
	}

	if sc.Expect.RootCause != "rds" {
		t.Errorf("expect.root_cause = %q, want %q", sc.Expect.RootCause, "rds")
	}

	// Check injected values are normalised to int.
	if v, ok := sc.Inject["api"]["restart_count"]; ok {
		if _, isInt := v.(int); !isInt {
			t.Errorf("inject.api.restart_count is %T, want int", v)
		}
	} else {
		t.Error("inject.api.restart_count missing")
	}

	// Check bool values are preserved.
	if v, ok := sc.Inject["rds"]["available"]; ok {
		if _, isBool := v.(bool); !isBool {
			t.Errorf("inject.rds.available is %T, want bool", v)
		}
	} else {
		t.Error("inject.rds.available missing")
	}
}

func TestRun_AllScenarios(t *testing.T) {
	root := repoRoot()
	m, reg := loadStorefront(t)

	scenarios, err := LoadAllScenarios(filepath.Join(root, "scenarios"))
	if err != nil {
		t.Fatalf("load scenarios: %v", err)
	}

	if len(scenarios) != 4 {
		t.Fatalf("loaded %d scenarios, want 4", len(scenarios))
	}

	for _, sc := range scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			result := Run(m, reg, sc)
			if !result.Pass {
				t.Errorf("scenario %q failed.\nExpected: root_cause=%q path=%v eliminated=%v\nActual:   root_cause=%q path=%v eliminated=%v",
					sc.Name,
					sc.Expect.RootCause, sc.Expect.Path, sc.Expect.Eliminated,
					result.Actual.RootCause, result.Actual.Path, result.Actual.Eliminated,
				)
			}
		})
	}
}
