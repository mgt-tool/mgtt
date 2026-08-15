// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// stageTestProvider writes a minimal provider (manifest.yaml + one
// type YAML in types/) under $MGTT_HOME/providers/<name>. Returns the
// provider directory.
func stageTestProvider(t *testing.T, home, providerName, typeName string) string {
	t.Helper()
	dir := filepath.Join(home, "providers", providerName)
	typesDir := filepath.Join(dir, "types")
	if err := os.MkdirAll(typesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte("meta:\n" +
		"  name: " + providerName + "\n" +
		"  version: 0.1.0\n" +
		"  description: test provider\n" +
		"install:\n" +
		"  source:\n" +
		"    build: hooks/install.sh\n" +
		"    clean: hooks/uninstall.sh\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	typeYAML := []byte("description: test type\n" +
		"facts:\n" +
		"  ready:\n" +
		"    type: mgtt.bool\n" +
		"    ttl: 30s\n" +
		"    probe:\n" +
		"      cmd: \"echo true\"\n" +
		"      parse: bool\n" +
		"      cost: low\n" +
		"healthy: [\"ready == true\"]\n" +
		"states:\n" +
		"  broken:\n" +
		"    when: \"ready == false\"\n" +
		"    description: broken\n" +
		"  live:\n" +
		"    when: \"ready == true\"\n" +
		"    description: live\n" +
		"default_active_state: live\n" +
		"failure_modes:\n" +
		"  broken:\n" +
		"    can_cause: [upstream_failure]\n")
	if err := os.WriteFile(filepath.Join(typesDir, typeName+".yaml"), typeYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeModel writes a minimal valid model that references providerName
// and typeName for a single component. Returns the model path.
func writeModel(t *testing.T, dir, modelName, providerName, typeName string) string {
	t.Helper()
	path := filepath.Join(dir, "model.yaml")
	body := "meta:\n" +
		"  name: " + modelName + "\n" +
		"  version: \"1.0\"\n" +
		"  providers:\n" +
		"    - " + providerName + "\n" +
		"components:\n" +
		"  svc:\n" +
		"    type: " + typeName + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// chdir cd's to dir for the test body and restores afterward. Thin
// wrapper around t.Chdir (Go 1.24+) so callers read uniformly.
func chdir(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
}

// runValidate invokes the cobra root with the given args and returns
// combined stdout+stderr and any execution error. Unlike testutil.RunCLI
// this returns errors (doesn't Fatal) so drift-detection tests can see
// them.
func runValidate(t *testing.T, args ...string) (string, error) {
	t.Helper()
	// RootCmd builds a fresh tree — flag state is per-invocation via the
	// command constructors, so no cross-call reset is needed.
	var buf bytes.Buffer
	cmd := RootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestValidate_WriteScenarios_SingleModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	stageTestProvider(t, home, "testprov", "svc_type")

	workDir := t.TempDir()
	modelPath := writeModel(t, workDir, "myapp", "testprov", "svc_type")

	out, err := runValidate(t, "model", "validate", modelPath, "--write-scenarios")
	if err != nil {
		t.Fatalf("validate failed: %v\noutput:\n%s", err, out)
	}
	scenariosPath := filepath.Join(workDir, "scenarios.yaml")
	if _, err := os.Stat(scenariosPath); err != nil {
		t.Fatalf("scenarios.yaml not written: %v\noutput:\n%s", err, out)
	}
	f, err := os.Open(scenariosPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, hash, err := scenarios.Read(f)
	if err != nil {
		t.Fatalf("parse scenarios.yaml: %v", err)
	}
	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("expected sha256: prefix hash, got %q", hash)
	}
	if !strings.Contains(out, "wrote") {
		t.Errorf("expected 'wrote' line in output, got:\n%s", out)
	}
}

func TestValidate_DetectsScenariosDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	stageTestProvider(t, home, "testprov", "svc_type")

	workDir := t.TempDir()
	modelPath := writeModel(t, workDir, "myapp", "testprov", "svc_type")

	// First write scenarios.yaml.
	if out, err := runValidate(t, "model", "validate", modelPath, "--write-scenarios"); err != nil {
		t.Fatalf("initial write failed: %v\n%s", err, out)
	}

	// Mutate the model so current content hash differs from stored one.
	body := "meta:\n" +
		"  name: myapp\n" +
		"  version: \"1.1\"\n" + // bumped
		"  providers:\n" +
		"    - testprov\n" +
		"components:\n" +
		"  svc:\n" +
		"    type: svc_type\n"
	if err := os.WriteFile(modelPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// Re-run validate WITHOUT --write-scenarios; expect a stale error.
	out, err := runValidate(t, "model", "validate", modelPath)
	if err == nil {
		t.Fatalf("expected stale-hash error; got nil\noutput:\n%s", out)
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Errorf("expected 'stale' in error; got %q", err.Error())
	}
}

// TestValidate_ReturnsErrorOnValidationErrors verifies that `mgtt model
// validate` returns a non-nil error (rather than os.Exit(1)'ing the test
// binary) when the loaded model fails validation.
func TestValidate_ReturnsErrorOnValidationErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	stageTestProvider(t, home, "testprov", "svc_type")

	workDir := t.TempDir()
	// Write a model whose component depends on a component that isn't
	// declared — pass3DepRefs flags this as a validation error.
	modelPath := filepath.Join(workDir, "model.yaml")
	body := "meta:\n" +
		"  name: bad\n" +
		"  version: \"1.0\"\n" +
		"  providers:\n" +
		"    - testprov\n" +
		"components:\n" +
		"  svc:\n" +
		"    type: svc_type\n" +
		"    depends:\n" +
		"      - on: [not_declared]\n"
	if err := os.WriteFile(modelPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runValidate(t, "model", "validate", modelPath)
	if err == nil {
		t.Fatalf("expected error on invalid model; got nil\noutput:\n%s", out)
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("expected 'validation' in error message; got %q", err.Error())
	}
}

// TestValidate_GenericFallback_LogsInfo verifies that when a model
// references an unknown type and strict_types is not set, validate
// succeeds and emits an INFO log line naming the component + type.
func TestValidate_GenericFallback_LogsInfo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	// No provider staged — the unknown type must resolve via generic
	// fallback.

	workDir := t.TempDir()
	modelPath := filepath.Join(workDir, "model.yaml")
	body := "meta:\n" +
		"  name: myapp\n" +
		"  version: \"1.0\"\n" +
		"components:\n" +
		"  widget:\n" +
		"    type: unknown_type\n"
	if err := os.WriteFile(modelPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runValidate(t, "model", "validate", modelPath)
	if err != nil {
		t.Fatalf("validate must succeed under default strict_types=false; got %v\nout:\n%s", err, out)
	}
	if !strings.Contains(out, "INFO: component \"widget\" uses generic.component") {
		t.Errorf("expected INFO log naming generic fallback; got:\n%s", out)
	}
	if !strings.Contains(out, "\"unknown_type\"") {
		t.Errorf("INFO log must name the unresolved type; got:\n%s", out)
	}
}

// TestValidate_StrictTypesRejectsGenericFallback verifies that
// meta.strict_types: true flips the generic-fallback INFO into a hard
// validation error.
func TestValidate_StrictTypesRejectsGenericFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)

	workDir := t.TempDir()
	modelPath := filepath.Join(workDir, "model.yaml")
	body := "meta:\n" +
		"  name: strict\n" +
		"  version: \"1.0\"\n" +
		"  strict_types: true\n" +
		"components:\n" +
		"  widget:\n" +
		"    type: unknown_type\n"
	if err := os.WriteFile(modelPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runValidate(t, "model", "validate", modelPath)
	if err == nil {
		t.Fatalf("strict_types: true must turn the fallback into an error; got nil\nout:\n%s", out)
	}
	if !strings.Contains(out, "strict_types") {
		t.Errorf("error output must name strict_types; got:\n%s", out)
	}
}

// TestValidate_ScenariosNoneSkipsDrift verifies that
// meta.scenarios: none exempts the model from needing a scenarios.yaml
// sidecar (and the drift check).
func TestValidate_ScenariosNoneSkipsDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	stageTestProvider(t, home, "testprov", "svc_type")

	workDir := t.TempDir()
	modelPath := filepath.Join(workDir, "model.yaml")
	// No scenarios.yaml is written; opted-out model should validate clean.
	body := "meta:\n" +
		"  name: placeholder\n" +
		"  version: \"1.0\"\n" +
		"  scenarios: none\n" +
		"  providers:\n" +
		"    - testprov\n" +
		"components:\n" +
		"  svc:\n" +
		"    type: svc_type\n"
	if err := os.WriteFile(modelPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runValidate(t, "model", "validate", modelPath)
	if err != nil {
		t.Fatalf("scenarios: none must skip drift; got %v\nout:\n%s", err, out)
	}
	// --write-scenarios should also not create a sidecar when opted out.
	out2, err := runValidate(t, "model", "validate", modelPath, "--write-scenarios")
	if err != nil {
		t.Fatalf("scenarios: none + --write-scenarios must not error; got %v\nout:\n%s", err, out2)
	}
	if _, statErr := os.Stat(filepath.Join(workDir, "scenarios.yaml")); !os.IsNotExist(statErr) {
		t.Errorf("scenarios: none must suppress scenarios.yaml generation; stat err=%v", statErr)
	}
}

// TestValidate_CheckScenariosAliasDetectsDrift verifies that
// --check-scenarios runs only the drift check (and catches stale
// sidecars), skipping other validation passes.
func TestValidate_CheckScenariosAliasDetectsDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	stageTestProvider(t, home, "testprov", "svc_type")

	workDir := t.TempDir()
	modelPath := writeModel(t, workDir, "myapp", "testprov", "svc_type")

	// Establish scenarios.yaml.
	if out, err := runValidate(t, "model", "validate", modelPath, "--write-scenarios"); err != nil {
		t.Fatalf("initial write: %v\n%s", err, out)
	}

	// Mutate the model; the sidecar hash is now stale.
	body := "meta:\n" +
		"  name: myapp\n" +
		"  version: \"1.2\"\n" +
		"  providers:\n" +
		"    - testprov\n" +
		"components:\n" +
		"  svc:\n" +
		"    type: svc_type\n"
	if err := os.WriteFile(modelPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runValidate(t, "model", "validate", modelPath, "--check-scenarios")
	if err == nil {
		t.Fatalf("expected drift error under --check-scenarios; got nil\nout:\n%s", out)
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Errorf("expected 'stale' in error; got %q", err.Error())
	}
}

// TestValidate_TriggeredByWarnsOnUnknownLabel verifies that a
// triggered_by label with no producer in the registry emits a WARNING
// (not an error). The test uses CollectGenericFallbacks indirectly by
// staging a provider with a state referencing a non-existent label.
func TestValidate_TriggeredByWarnsOnUnknownLabel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)

	// Stage a provider whose state references a label no can_cause
	// produces.
	dir := filepath.Join(home, "providers", "tp")
	typesDir := filepath.Join(dir, "types")
	if err := os.MkdirAll(typesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte("meta:\n" +
		"  name: tp\n" +
		"  version: 0.1.0\n" +
		"  description: test\n" +
		"install:\n" +
		"  source:\n" +
		"    build: hooks/install.sh\n" +
		"    clean: hooks/uninstall.sh\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	typeYAML := []byte("description: s\n" +
		"facts:\n" +
		"  ready:\n" +
		"    type: mgtt.bool\n" +
		"    probe:\n" +
		"      cmd: echo\n" +
		"states:\n" +
		"  broken:\n" +
		"    triggered_by: [nobody_produces_me]\n" +
		"    description: broken\n" +
		"default_active_state: broken\n")
	if err := os.WriteFile(filepath.Join(typesDir, "svc.yaml"), typeYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	workDir := t.TempDir()
	modelPath := writeModel(t, workDir, "myapp", "tp", "svc")

	out, err := runValidate(t, "model", "validate", modelPath)
	if err != nil {
		t.Fatalf("unknown triggered_by label must be WARNING not ERROR; got %v\nout:\n%s", err, out)
	}
	if !strings.Contains(out, "nobody_produces_me") {
		t.Errorf("expected warning naming unknown label; got:\n%s", out)
	}
	if !strings.Contains(out, "has no producer") {
		t.Errorf("expected 'has no producer' wording; got:\n%s", out)
	}
}

func TestValidate_MultiModelIndex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	stageTestProvider(t, home, "testprov", "svc_type")

	workRoot := t.TempDir()
	aDir := filepath.Join(workRoot, "a")
	bDir := filepath.Join(workRoot, "b")
	if err := os.MkdirAll(aDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeModel(t, aDir, "app-a", "testprov", "svc_type")
	writeModel(t, bDir, "app-b", "testprov", "svc_type")

	chdir(t, workRoot)

	out, err := runValidate(t, "model", "validate", "--write-scenarios")
	if err != nil {
		t.Fatalf("multi-model validate failed: %v\noutput:\n%s", err, out)
	}

	if _, err := os.Stat(filepath.Join(aDir, "scenarios.yaml")); err != nil {
		t.Errorf("a/scenarios.yaml not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bDir, "scenarios.yaml")); err != nil {
		t.Errorf("b/scenarios.yaml not written: %v", err)
	}
	idxPath := filepath.Join(workRoot, "scenarios.index.yaml")
	f, err := os.Open(idxPath)
	if err != nil {
		t.Fatalf("scenarios.index.yaml not written: %v\noutput:\n%s", err, out)
	}
	defer f.Close()
	entries, err := scenarios.ReadIndex(f)
	if err != nil {
		t.Fatalf("parse index: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("want 2 index entries, got %d: %+v", len(entries), entries)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	if !names["app-a"] || !names["app-b"] {
		t.Errorf("expected both app-a and app-b in index; got %+v", entries)
	}
}
