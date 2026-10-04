// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package modeldiff

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

func registry(t *testing.T) *providersupport.Registry {
	t.Helper()
	reg := providersupport.NewRegistry()
	for _, f := range []string{"compute.yaml", "datalayer.yaml"} {
		p, err := providersupport.LoadFromFile(filepath.Join("..", "..", "testdata", "providers", f))
		if err != nil {
			t.Fatal(err)
		}
		reg.Register(p)
	}
	return reg
}

func load(t *testing.T, edgeDeps, storeHealthy string) *model.Model {
	t.Helper()
	src := `meta:
  name: d
  version: "1"
  providers: [compute, datalayer]
components:
  edge:
    type: gateway
    depends:
` + edgeDeps + `
  web-a:
    type: workload
    depends:
      - on: store
  web-b:
    type: workload
    depends:
      - on: store
  store:
    providers: [datalayer]
    type: datastore
` + storeHealthy
	m, err := model.LoadBytes([]byte(src), "")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

const (
	group = "      - on: [web-a, web-b]\n        need: 1\n"
	hard  = "      - on: [web-a, web-b]\n"
)

func TestCompare_SameMeaningIsEmpty(t *testing.T) {
	reg := registry(t)
	if d := Compare(load(t, group, ""), load(t, group, ""), reg); !d.Empty() {
		t.Fatalf("identical models differ: %+v", d)
	}
}

// Dropping a group changes no symptom set a text diff would name, but
// one web's failure now reaches the edge.
func TestCompare_GroupRemovedReachesUsers(t *testing.T) {
	reg := registry(t)
	d := Compare(load(t, group, ""), load(t, hard, ""), reg)
	var reach []string
	for _, r := range d.Reach {
		if reflect.DeepEqual(r.Gained, []string{"edge"}) {
			reach = append(reach, r.Component)
		}
	}
	if !reflect.DeepEqual(reach, []string{"web-a", "web-b"}) {
		t.Fatalf("want web-a and web-b to newly reach edge; got %+v", d.Reach)
	}
	if len(d.Changed) != 1 || d.Changed[0].Name != "edge" {
		t.Fatalf("want edge's dependencies changed; got %+v", d.Changed)
	}
}

// Effective rules are compared, so dropping a type rule through an
// override shows, and respelling the same rules does not.
func TestCompare_EffectiveHealthRules(t *testing.T) {
	reg := registry(t)
	base := load(t, group, "")
	dropped := load(t, group, "    healthy: [connection_count < 500]\n")
	d := Compare(base, dropped, reg)
	if len(d.Changed) != 1 || !strings.Contains(strings.Join(d.Changed[0].Changes, "\n"), "healthy no longer requires available == true") {
		t.Fatalf("want store's dropped rule named; got %+v", d.Changed)
	}
	restated := load(t, group, "    healthy:\n      replace: [available==true, connection_count<500]\n")
	if d := Compare(base, restated, reg); !d.Empty() {
		t.Fatalf("restating the type's rules changes nothing; got %+v", d)
	}
}
