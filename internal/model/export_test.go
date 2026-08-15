// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// loadSampleForExport stages the four-component storefront model against the
// two fictional providers the rest of this package's tests use.
func loadSampleForExport(t *testing.T) (*Model, *providersupport.Registry) {
	t.Helper()
	m, err := Load(testdataPath("sample-model.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	reg := providersupport.NewRegistry()
	for _, file := range []string{
		"../../testdata/providers/compute.yaml",
		"../../testdata/providers/datalayer.yaml",
	} {
		p, err := providersupport.LoadFromFile(file)
		if err != nil {
			t.Fatalf("LoadFromFile(%s): %v", file, err)
		}
		reg.Register(p)
	}
	return m, reg
}

type exportProbe struct {
	Version    int    `json:"mgtt_export_version"`
	Name       string `json:"name"`
	Components []struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Healthy []string
		Depends []struct {
			On    string `json:"on"`
			While string `json:"while"`
		} `json:"depends"`
	} `json:"components"`
	Types []struct {
		Name               string `json:"name"`
		Provider           string `json:"provider"`
		DefaultActiveState string `json:"default_active_state"`
		Facts              []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"facts"`
		States []struct {
			Name string `json:"name"`
			When string `json:"when"`
		} `json:"states"`
	} `json:"types"`
}

func exportProbeOf(t *testing.T) exportProbe {
	t.Helper()
	m, reg := loadSampleForExport(t)
	body, err := ExportJSON(m, reg)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var doc exportProbe
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("emitted JSON does not parse: %v\n%s", err, body)
	}
	return doc
}

// A component whose type resolves to the embedded generic fallback carries no
// facts and no states, so a consumer would build a model of nothing and report
// it clean. The export must say so rather than pass it on in silence.
func TestExportJSON_DeclinesGenericFallback(t *testing.T) {
	m, err := Load(testdataPath("sample-model.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// A registry with the real providers absent: every type falls back.
	reg := providersupport.NewRegistry()
	gen, err := providersupport.LoadFromBytes([]byte(
		"meta:\n" +
			"  name: generic\n" +
			"  version: 0.1.0\n" +
			"  description: fallback\n" +
			"install:\n" +
			"  source:\n" +
			"    build: hooks/install.sh\n" +
			"    clean: hooks/uninstall.sh\n" +
			"types:\n" +
			"  component:\n" +
			"    facts: {}\n" +
			"    states: {}\n"))
	if err != nil {
		t.Fatalf("stage generic provider: %v", err)
	}
	reg.Register(gen)

	body, err := ExportJSON(m, reg)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var doc struct {
		Declines []struct {
			What string `json:"what"`
			Why  string `json:"why"`
		} `json:"declines"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("emitted JSON does not parse: %v", err)
	}
	if len(doc.Declines) != 4 {
		t.Fatalf("declines = %+v, want one per component (4)", doc.Declines)
	}
	if !strings.Contains(doc.Declines[0].Why, "generic") {
		t.Errorf("decline should name the generic fallback, got %q", doc.Declines[0].Why)
	}
}

// The happy path declines nothing — otherwise the signal is worthless.
func TestExportJSON_NoDeclinesWhenTypesResolve(t *testing.T) {
	m, reg := loadSampleForExport(t)
	body, err := ExportJSON(m, reg)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var doc struct {
		Declines []map[string]string `json:"declines"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("emitted JSON does not parse: %v", err)
	}
	if len(doc.Declines) != 0 {
		t.Errorf("declines = %+v, want none when every type resolves", doc.Declines)
	}
}

func TestExportJSON_Deterministic(t *testing.T) {
	m, reg := loadSampleForExport(t)

	first, err := ExportJSON(m, reg)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := ExportJSON(m, reg)
		if err != nil {
			t.Fatalf("ExportJSON (run %d): %v", i, err)
		}
		if string(first) != string(again) {
			t.Fatalf("ExportJSON is not deterministic (run %d differs)", i)
		}
	}
}

func TestExportJSON_VersionAndName(t *testing.T) {
	doc := exportProbeOf(t)
	if doc.Version != ExportVersion {
		t.Errorf("mgtt_export_version = %d, want %d", doc.Version, ExportVersion)
	}
	if doc.Name != "storefront" {
		t.Errorf("name = %q, want storefront", doc.Name)
	}
}

func TestExportJSON_ComponentsSortedByName(t *testing.T) {
	doc := exportProbeOf(t)
	if len(doc.Components) != 4 {
		t.Fatalf("got %d components, want 4", len(doc.Components))
	}
	want := []string{"api", "edge", "frontend", "store"}
	for i, w := range want {
		if doc.Components[i].Name != w {
			t.Errorf("components[%d] = %q, want %q (sorted by name)", i, doc.Components[i].Name, w)
		}
	}
}

// The store component overrides healthy at the component level; the export must
// carry the effective list, so the reader never re-implements mgtt's precedence.
func TestExportJSON_ComponentHealthyIsEffective(t *testing.T) {
	doc := exportProbeOf(t)
	for _, c := range doc.Components {
		if c.Name != "store" {
			continue
		}
		if len(c.Healthy) != 2 {
			t.Fatalf("store.healthy = %v, want the 2 component-level clauses", c.Healthy)
		}
		if c.Healthy[0] != "available == true" {
			t.Errorf("store.healthy[0] = %q, want %q", c.Healthy[0], "available == true")
		}
		return
	}
	t.Fatal("no store component in the export")
}

// A component with no healthy override inherits the type's list.
func TestExportJSON_ComponentHealthyInheritsFromType(t *testing.T) {
	doc := exportProbeOf(t)
	for _, c := range doc.Components {
		if c.Name != "api" {
			continue
		}
		if len(c.Healthy) != 3 {
			t.Fatalf("api.healthy = %v, want the workload type's 3 clauses", c.Healthy)
		}
		return
	}
	t.Fatal("no api component in the export")
}

func TestExportJSON_DependenciesFlattened(t *testing.T) {
	doc := exportProbeOf(t)
	for _, c := range doc.Components {
		if c.Name != "edge" {
			continue
		}
		if len(c.Depends) != 2 {
			t.Fatalf("edge.depends = %v, want 2 flattened edges", c.Depends)
		}
		if c.Depends[0].On != "api" || c.Depends[1].On != "frontend" {
			t.Errorf("edge.depends = %v, want api then frontend (sorted)", c.Depends)
		}
		return
	}
	t.Fatal("no edge component in the export")
}

func TestExportJSON_TypesCarryFactsAndStates(t *testing.T) {
	doc := exportProbeOf(t)
	if len(doc.Types) != 3 {
		t.Fatalf("got %d types, want 3 (datastore, gateway, workload)", len(doc.Types))
	}
	var ds *struct {
		Name               string `json:"name"`
		Provider           string `json:"provider"`
		DefaultActiveState string `json:"default_active_state"`
		Facts              []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"facts"`
		States []struct {
			Name string `json:"name"`
			When string `json:"when"`
		} `json:"states"`
	}
	for i := range doc.Types {
		if doc.Types[i].Name == "datastore" {
			ds = &doc.Types[i]
		}
	}
	if ds == nil {
		t.Fatal("no datastore type in the export")
	}
	if ds.Provider != "datalayer" {
		t.Errorf("datastore.provider = %q, want datalayer", ds.Provider)
	}
	if ds.DefaultActiveState != "live" {
		t.Errorf("datastore.default_active_state = %q, want live", ds.DefaultActiveState)
	}
	if len(ds.Facts) != 2 {
		t.Fatalf("datastore.facts = %v, want available and connection_count", ds.Facts)
	}
	if ds.Facts[0].Name != "available" || ds.Facts[0].Type != "mgtt.bool" {
		t.Errorf("datastore.facts[0] = %+v, want available/mgtt.bool (sorted)", ds.Facts[0])
	}
	if len(ds.States) != 2 {
		t.Fatalf("datastore.states = %v, want live and stopped", ds.States)
	}
	// States keep DECLARED order — the emitter's witness choice depends on it.
	if ds.States[0].Name != "live" {
		t.Errorf("datastore.states[0] = %q, want live (declared order)", ds.States[0].Name)
	}
	if ds.States[0].When != "available == true" {
		t.Errorf("datastore.states[0].when = %q, want %q", ds.States[0].When, "available == true")
	}
}
