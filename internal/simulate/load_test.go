// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeScenario(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadScenario_Unresolved(t *testing.T) {
	sc, err := LoadScenario(writeScenario(t, `name: refused
unresolved:
  rds: { available: forbidden, connection_count: not_found }
expect:
  root_cause: none
`))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Unresolved["rds"]["available"] != "forbidden" || sc.Unresolved["rds"]["connection_count"] != "not_found" {
		t.Fatalf("unresolved = %+v", sc.Unresolved)
	}
}

// A status no probe records, or a fact that is both a value and a probe
// failure, is a mistake in the scenario and fails to load.
func TestLoadScenario_UnresolvedRejectsMistakes(t *testing.T) {
	for name, body := range map[string]string{
		"unknown status": "name: x\nunresolved:\n  rds: { available: denied }\n",
		"both":           "name: x\ninject:\n  rds: { available: true }\nunresolved:\n  rds: { available: forbidden }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadScenario(writeScenario(t, body))
			if err == nil || !strings.Contains(err.Error(), "rds.available") {
				t.Fatalf("want an error naming rds.available, got %v", err)
			}
		})
	}
}
