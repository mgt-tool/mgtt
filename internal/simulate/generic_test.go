// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"testing"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/genericprovider"
)

// A component no provider types falls back to the generic component, whose
// only evidence is the operator's word. "Not healthy" must name it, not
// clear it; "healthy" must clear it.
func TestRun_GenericComponentTakesTheOperatorsWord(t *testing.T) {
	reg := providersupport.NewRegistry()
	if err := genericprovider.Register(reg); err != nil {
		t.Fatal(err)
	}
	m, err := model.LoadBytes([]byte(`meta:
  name: g
  version: "1.0"
components:
  front:
    type: thing
    depends:
      - on: back
  back:
    type: thing
`), "system.model.yaml")
	if err != nil {
		t.Fatal(err)
	}
	says := func(front, back bool) *Scenario {
		return &Scenario{Inject: map[string]map[string]any{
			"front": {"operator_says_healthy": front},
			"back":  {"operator_says_healthy": back},
		}}
	}
	if got := Run(m, reg, says(false, false)).Actual; got.RootCause != "back" {
		t.Errorf("both confirmed broken: root cause %q, eliminated %v; want back", got.RootCause, got.Eliminated)
	}
	if got := Run(m, reg, says(true, true)).Actual; got.RootCause != "none" || len(got.Eliminated) != 2 {
		t.Errorf("both confirmed healthy: root cause %q, eliminated %v; want none, both cleared", got.RootCause, got.Eliminated)
	}
}
