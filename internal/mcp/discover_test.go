// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// stubDiscovery installs a provider whose discovery returns body.
func stubDiscovery(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, "providers", "kubernetes", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = \"discover\" ]; then\n  cat <<'EOF'\n" + body + "\nEOF\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "provider"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// model_discover proposes the model discovery implies, merged with the
// existing one: what is new, what discovery no longer finds, and the
// authored components it keeps. It writes nothing.
func TestModelDiscover(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	stubDiscovery(t, home, `{"components":[{"name":"api","type":"deployment"},{"name":"web","type":"deployment"}]}`)
	existing := filepath.Join(t.TempDir(), "system.model.yaml")
	body := `meta: { name: m, version: "1", providers: [kubernetes] }
components:
  api: { type: deployment, source: discovered }
  old-svc: { type: service, source: discovered }
  checkout:
    type: business_process
    depends:
      - on: api
      - on: gone
`
	if err := os.WriteFile(existing, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := NewHandler(Config{}).ModelDiscover(ModelDiscoverParams{ModelPath: existing})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Added, []string{"web"}) || !reflect.DeepEqual(res.Removed, []string{"old-svc"}) {
		t.Errorf("added %v removed %v, want [web] [old-svc]", res.Added, res.Removed)
	}
	if !reflect.DeepEqual(res.KeptAuthored, []string{"checkout"}) || !reflect.DeepEqual(res.Dangling, []string{"checkout depends on gone"}) {
		t.Errorf("kept %v dangling %v", res.KeptAuthored, res.Dangling)
	}
	if !strings.Contains(res.ProposedModel, "checkout:") || !strings.Contains(res.ProposedModel, "source: discovered") {
		t.Errorf("proposed model:\n%s", res.ProposedModel)
	}
	if after, _ := os.ReadFile(existing); string(after) != body {
		t.Error("model_discover must not write the model")
	}
}

// Discovery reads live systems, so it is served only where the
// providers' credentials are: never in the authoring toolset.
func TestModelDiscover_DiagnoseToolsetOnly(t *testing.T) {
	if _, ok := buildServer(Config{Toolset: "authoring"}).ListTools()["model_discover"]; ok {
		t.Error("model_discover must not be served in the authoring toolset")
	}
	if _, ok := buildServer(Config{Toolset: "diagnose"}).ListTools()["model_discover"]; !ok {
		t.Error("model_discover belongs to the diagnose toolset")
	}
}
