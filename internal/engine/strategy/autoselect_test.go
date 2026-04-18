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
