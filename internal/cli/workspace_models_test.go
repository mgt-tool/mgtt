// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFindWorkspaceModels_MatchesModelSuffix guards the onboarding path:
// `mgtt model validate --write-scenarios` (no path arg) must discover the
// system.model.yaml scaffolded by `mgtt init` and any *.model.yaml from
// the multi-file methodology — not just a literal model.yaml. A regression
// here hard-fails the documented quickstart for every first-time author.
func TestFindWorkspaceModels_MatchesModelSuffix(t *testing.T) {
	dir := t.TempDir()
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("meta: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("model.yaml")
	write("system.model.yaml")
	write("payments.model.yaml")
	write("scenarios.yaml") // must NOT be treated as a model
	write("notes.txt")

	got, err := findWorkspaceModels(dir)
	if err != nil {
		t.Fatalf("findWorkspaceModels: %v", err)
	}

	found := map[string]bool{}
	for _, p := range got {
		found[filepath.Base(p)] = true
	}

	for _, want := range []string{"model.yaml", "system.model.yaml", "payments.model.yaml"} {
		if !found[want] {
			t.Errorf("expected %s among workspace models, got %v", want, got)
		}
	}
	if found["scenarios.yaml"] {
		t.Errorf("scenarios.yaml must not be treated as a model")
	}
}
