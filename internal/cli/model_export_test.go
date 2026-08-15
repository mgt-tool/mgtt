// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stageExportWorkspace lays out $MGTT_HOME with one provider and a model
// beside it, then cd's into the model's directory. Returns that directory.
func stageExportWorkspace(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	stageTestProvider(t, home, "testprov", "svc_type")
	dir := t.TempDir()
	writeModel(t, dir, "exported", "testprov", "svc_type")
	chdir(t, dir)
	return dir
}

func TestModelExport_RequiresJSONFlag(t *testing.T) {
	stageExportWorkspace(t)
	_, err := runValidate(t, "model", "export", "model.yaml")
	if err == nil {
		t.Fatal("expected an error without --json")
	}
	if !strings.Contains(err.Error(), "--json") {
		t.Errorf("error should name the missing flag, got: %v", err)
	}
}

func TestModelExport_EmitsParsableDocument(t *testing.T) {
	stageExportWorkspace(t)

	out, err := runValidate(t, "model", "export", "--json", "model.yaml")
	if err != nil {
		t.Fatalf("model export --json: %v (output: %s)", err, out)
	}

	var doc struct {
		Version    int    `json:"mgtt_export_version"`
		Name       string `json:"name"`
		Components []struct {
			Name    string   `json:"name"`
			Type    string   `json:"type"`
			Healthy []string `json:"healthy"`
		} `json:"components"`
		Types []struct {
			Name  string `json:"name"`
			Facts []struct {
				Name string `json:"name"`
			} `json:"facts"`
		} `json:"types"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if doc.Version != 1 {
		t.Errorf("mgtt_export_version = %d, want 1", doc.Version)
	}
	if doc.Name != "exported" {
		t.Errorf("name = %q, want exported", doc.Name)
	}
	if len(doc.Components) != 1 || doc.Components[0].Name != "svc" {
		t.Fatalf("components = %+v, want one named svc", doc.Components)
	}
	// The component has no healthy override, so it inherits the type's.
	if len(doc.Components[0].Healthy) != 1 {
		t.Errorf("svc.healthy = %v, want the type's single clause", doc.Components[0].Healthy)
	}
	if len(doc.Types) != 1 || len(doc.Types[0].Facts) != 1 {
		t.Fatalf("types = %+v, want one type carrying one fact", doc.Types)
	}
}

func TestModelExport_WritesToOutputPath(t *testing.T) {
	dir := stageExportWorkspace(t)
	target := filepath.Join(dir, "resolved.json")

	if _, err := runValidate(t, "model", "export", "--json", "--output", target, "model.yaml"); err != nil {
		t.Fatalf("model export --output: %v", err)
	}

	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("written file is not JSON: %v", err)
	}
	if doc["mgtt_export_version"] != float64(1) {
		t.Errorf("mgtt_export_version = %v, want 1", doc["mgtt_export_version"])
	}
}
