// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/model/build"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

func loadModelYAML(t *testing.T, components string) (*model.Model, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "system.model.yaml")
	body := "meta:\n  name: h\n  version: \"1.0\"\n  providers: [aws]\ncomponents:\n" + components
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return model.Load(path)
}

var rdsType = &providersupport.Type{
	Name:       "rds_instance",
	HealthyRaw: []string{"available == true", "connection_count < 500"},
}

// healthy: is a bare list (replace, the old spelling), replace:, or add:.
func TestHealthyForms(t *testing.T) {
	m, err := loadModelYAML(t, `  bare:
    type: rds_instance
    healthy: [connection_count < 900]
  replaced:
    type: rds_instance
    healthy:
      replace: [connection_count < 900]
  added:
    type: rds_instance
    healthy:
      add: [replica_lag < 30]
  inherited:
    type: rds_instance
`)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]string{
		"bare":      {"connection_count < 900"},
		"replaced":  {"connection_count < 900"},
		"added":     {"available == true", "connection_count < 500", "replica_lag < 30"},
		"inherited": {"available == true", "connection_count < 500"},
	} {
		if got := m.Components[name].HealthyRulesRaw(rdsType); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: rules %q, want %q", name, got, want)
		}
	}
	if len(m.Components["added"].Healthy) != 1 {
		t.Errorf("added: own rules must compile; got %d", len(m.Components["added"].Healthy))
	}
}

func TestHealthyForms_Rejected(t *testing.T) {
	for name, block := range map[string]string{
		"both keys":   "      replace: [a == 1]\n      add: [b == 1]\n",
		"unknown key": "      merge: [a == 1]\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadModelYAML(t, "  db:\n    type: rds_instance\n    healthy:\n"+block)
			if err == nil || !strings.Contains(err.Error(), "healthy:") {
				t.Fatalf("want a healthy: error, got %v", err)
			}
		})
	}
}

// A bare list still warns about the type rules it drops; replace: says the
// drop is deliberate and add: drops nothing, so neither warns.
func TestValidate_HealthyOverrideWarnsOnlyForBareList(t *testing.T) {
	m, err := loadModelYAML(t, `  bare:
    type: rds_instance
    healthy: [connection_count < 900]
  replaced:
    type: rds_instance
    healthy:
      replace: [connection_count < 900]
  added:
    type: rds_instance
    healthy:
      add: [replica_lag < 30]
`)
	if err != nil {
		t.Fatal(err)
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "aws"},
		Types: map[string]*providersupport.Type{"rds_instance": rdsType},
	})
	bareWarned := false
	for _, w := range model.Validate(m, reg).Warnings {
		if w.Field != "healthy" {
			continue
		}
		if w.Component != "bare" {
			t.Errorf("unexpected warning on %s: %s", w.Component, w.Message)
		}
		bareWarned = true
	}
	if !bareWarned {
		t.Error("the bare list drops type rules and should still warn")
	}
}

// model build re-emits a component it keeps; replace: and add: must
// survive, since flattening either to a bare list changes its meaning.
func TestEmitYAML_KeepsHealthyMode(t *testing.T) {
	m, err := loadModelYAML(t, `  replaced:
    type: rds_instance
    healthy:
      replace: [connection_count < 900]
  added:
    type: rds_instance
    healthy:
      add: [replica_lag < 30]
`)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := build.EmitYAML(m, &b); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "re.yaml")
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := model.Load(path)
	if err != nil {
		t.Fatalf("re-load emitted model: %v\n%s", err, b.String())
	}
	if again.Components["replaced"].HealthyMode != "replace" || again.Components["added"].HealthyMode != "add" {
		t.Fatalf("modes lost in emit:\n%s", b.String())
	}
}
