// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"testing"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
)

// A Service keyed by kind reads the Service it names: the probe's resource
// and its command's {name} drop the prefix, as kubectl reads service/acme.
func TestProbe_KindPrefixedKeyReadsItsResource(t *testing.T) {
	ty := &providersupport.Type{
		Name:  "service",
		Facts: map[string]*providersupport.FactSpec{"endpoint_count": {TypeName: "mgtt.int", Probe: providersupport.ProbeDef{Cmd: "kubectl get endpoints {name}"}}},
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{Meta: providersupport.ProviderMeta{Name: "kubernetes"}, Types: map[string]*providersupport.Type{"service": ty}})
	m := &model.Model{
		Meta:       model.Meta{Providers: []string{"kubernetes"}},
		Components: map[string]*model.Component{"service/acme": {Name: "service/acme", Type: "service"}},
		Order:      []string{"service/acme"},
	}
	p := firstUncollectedFact(Input{Model: m, Registry: reg}, "service/acme", ty, "kubernetes", m.Components["service/acme"])
	if p == nil || p.Resource != "acme" || p.Target() != "acme" {
		t.Fatalf("got %+v; want resource acme", p)
	}
	if got := probe.Substitute(p.Command, p.Target(), nil, nil); got != "kubectl get endpoints acme" {
		t.Errorf("rendered %q", got)
	}
}
