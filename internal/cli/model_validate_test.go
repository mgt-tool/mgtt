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

// chdir cd's to dir for the test body and restores afterward.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// runValidate invokes the cobra root with the given args and returns
// combined stdout+stderr and any execution error. Unlike testutil.RunCLI
// this returns errors (doesn't Fatal) so drift-detection tests can see
// them.
func runValidate(t *testing.T, args ...string) (string, error) {
	t.Helper()
	// Reset the package-level flag between calls since cobra binds to it
	// via BoolVar (persistent state across Execute calls).
	writeScenariosFlag = false
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
