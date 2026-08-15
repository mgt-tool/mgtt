// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"testing"

	"github.com/mgt-tool/mgtt/internal/scenarios"
)

func TestAutoSelect_OccamWhenScenariosPresent(t *testing.T) {
	s := AutoSelect(Input{Scenarios: []scenarios.Scenario{{ID: "s-1"}}})
	if s.Name() != "occam" {
		t.Errorf("want occam; got %s", s.Name())
	}
}

func TestAutoSelect_BFSWhenScenariosAbsent(t *testing.T) {
	s := AutoSelect(Input{Scenarios: nil})
	if s.Name() != "bfs" {
		t.Errorf("want bfs; got %s", s.Name())
	}
}
