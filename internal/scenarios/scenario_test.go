// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package scenarios

import "testing"

func TestStep_Terminal(t *testing.T) {
	s := Step{Component: "nginx", State: "degraded", Observes: []string{"upstream_count"}}
	if !s.IsTerminal() {
		t.Error("step with observes should be terminal")
	}
	nonTerm := Step{Component: "rds", State: "stopped", EmitsOnEdge: "query_timeout"}
	if nonTerm.IsTerminal() {
		t.Error("step with emits_on_edge should not be terminal")
	}
}

func TestScenario_Length(t *testing.T) {
	s := Scenario{Chain: []Step{
		{Component: "rds", State: "stopped", EmitsOnEdge: "query_timeout"},
		{Component: "api", State: "crash_looping", EmitsOnEdge: "upstream_failure"},
		{Component: "nginx", State: "degraded", Observes: []string{"upstream_count"}},
	}}
	if s.Length() != 3 {
		t.Errorf("want length 3; got %d", s.Length())
	}
}

func TestScenario_TouchesComponent(t *testing.T) {
	s := Scenario{Chain: []Step{
		{Component: "rds", State: "stopped"},
		{Component: "api", State: "crash_looping"},
	}}
	if !s.TouchesComponent("api") {
		t.Error("should touch api")
	}
	if s.TouchesComponent("cache") {
		t.Error("should not touch cache")
	}
}
