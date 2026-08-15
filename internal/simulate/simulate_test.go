// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"testing"
)

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
