// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// A derived fact is an ordinary fact to the engine: a scenario injects
// its value like any other, and health can rest on growth rather than on a
// level. Here a broker is judged by how fast its queue grows: a deep but
// flat queue is healthy, a shallow one climbing fast is the root cause.
func TestRun_DerivedFactsAreInjectedLikeAnyOther(t *testing.T) {
	parse := func(s string) expr.Node {
		n, err := expr.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	broker := &providersupport.Type{
		Name: "broker",
		Facts: map[string]*providersupport.FactSpec{
			"queue_depth":          {TypeName: "mgtt.int"},
			"queue_depth_delta_5m": {TypeName: "mgtt.int", Window: 5 * time.Minute, Derive: "delta"},
		},
		Healthy: []expr.Node{parse("queue_depth_delta_5m < 1000")},
		States: []providersupport.StateDef{
			{Name: "live", When: parse("queue_depth_delta_5m < 1000")},
			{Name: "backlogged", When: parse("queue_depth_delta_5m >= 1000")},
		},
		DefaultActiveState: "live",
		FailureModes:       map[string][]string{"backlogged": {"upstream_failure"}},
	}
	app := &providersupport.Type{
		Name:               "app",
		Facts:              map[string]*providersupport.FactSpec{"up": {TypeName: "mgtt.bool"}},
		Healthy:            []expr.Node{parse("up == true")},
		States:             []providersupport.StateDef{{Name: "live", When: parse("up == true")}, {Name: "stopped", When: parse("up == false")}},
		DefaultActiveState: "live",
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{Meta: providersupport.ProviderMeta{Name: "p"}, Types: map[string]*providersupport.Type{"broker": broker, "app": app}})
	m := &model.Model{
		Meta: model.Meta{Providers: []string{"p"}},
		Components: map[string]*model.Component{
			"worker": {Name: "worker", Type: "app", Depends: []model.Dependency{{On: []string{"mq"}}}},
			"mq":     {Name: "mq", Type: "broker"},
		},
		Order: []string{"worker", "mq"},
	}
	m.BuildGraph()
	run := func(depth, delta int) string {
		return Run(m, reg, &Scenario{Inject: map[string]map[string]any{
			"worker": {"up": false},
			"mq":     {"queue_depth": depth, "queue_depth_delta_5m": delta},
		}}).Actual.RootCause
	}
	if got := run(9000, 10); got != "worker" {
		t.Errorf("a deep, flat queue is healthy, so the worker is the root cause; got %q", got)
	}
	if got := run(300, 4000); got != "mq" {
		t.Errorf("a shallow queue climbing 4000 in 5m is the root cause; got %q", got)
	}
}
