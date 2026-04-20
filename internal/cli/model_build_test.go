package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// End-to-end: empty home, empty model output, exit 0.
func TestModelBuild_EmptyHome(t *testing.T) {
	home := t.TempDir()
	out := t.TempDir()
	outFile := filepath.Join(out, "system.model.yaml")
	var stdout, stderr bytes.Buffer
	code := runModelBuild(context.Background(), modelBuildFlags{
		mgttHome: home,
		output:   outFile,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d; stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "components:") {
		t.Errorf("output should be a valid (if empty) model; got: %s", data)
	}
}

// End-to-end: one stub provider with one component.
func TestModelBuild_SingleProvider(t *testing.T) {
	home := t.TempDir()
	installStubProviderInline(t, home, "kubernetes", `{"components":[{"name":"api","type":"deployment"}]}`)

	out := filepath.Join(t.TempDir(), "system.model.yaml")
	var stdout, stderr bytes.Buffer
	code := runModelBuild(context.Background(), modelBuildFlags{
		mgttHome: home,
		output:   out,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d; stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "api:") {
		t.Errorf("output should contain api component; got: %s", data)
	}
}

// Deletion gate behavior: creates, then tries to shrink (blocked),
// then succeeds with --allow-deletes.
func TestModelBuild_DeletionGate(t *testing.T) {
	home := t.TempDir()
	installStubProviderInline(t, home, "kubernetes", `{"components":[{"name":"api","type":"deployment"},{"name":"old-svc","type":"service"}]}`)

	outDir := t.TempDir()
	out := filepath.Join(outDir, "system.model.yaml")

	// First build: creates the model.
	var stdout1, stderr1 bytes.Buffer
	code1 := runModelBuild(context.Background(), modelBuildFlags{mgttHome: home, output: out}, &stdout1, &stderr1)
	if code1 != 0 {
		t.Fatalf("first build: code=%d stderr=%s", code1, stderr1.String())
	}

	// Now change the stub to remove old-svc.
	installStubProviderInline(t, home, "kubernetes", `{"components":[{"name":"api","type":"deployment"}]}`)

	// Second build without --allow-deletes: must refuse.
	var stdout2, stderr2 bytes.Buffer
	code2 := runModelBuild(context.Background(), modelBuildFlags{mgttHome: home, output: out}, &stdout2, &stderr2)
	if code2 == 0 {
		t.Fatal("second build must refuse deletion; got exit 0")
	}
	if !strings.Contains(stderr2.String(), "old-svc") {
		t.Errorf("stderr should mention removed component; got: %s", stderr2.String())
	}

	// Third build with --allow-deletes: succeeds.
	var stdout3, stderr3 bytes.Buffer
	code3 := runModelBuild(context.Background(), modelBuildFlags{mgttHome: home, output: out, allowDeletes: true}, &stdout3, &stderr3)
	if code3 != 0 {
		t.Fatalf("third build with --allow-deletes: code=%d stderr=%s", code3, stderr3.String())
	}
}

// Tombstone semantics: a component named in --tombstone that was in
// prev but isn't in current discovery MUST survive the rebuild,
// preserved verbatim. This is the flag's whole purpose — air-gapped
// infra and partial discovery failures use it to keep the model's
// picture of reality complete across rebuilds.
func TestModelBuild_TombstonePreservesComponent(t *testing.T) {
	home := t.TempDir()
	installStubProviderInline(t, home, "kubernetes", `{"components":[{"name":"api","type":"deployment"},{"name":"air-gapped-db","type":"rds_instance"}]}`)

	out := filepath.Join(t.TempDir(), "system.model.yaml")

	// First build: both components in the model.
	var stdout1, stderr1 bytes.Buffer
	code1 := runModelBuild(context.Background(), modelBuildFlags{mgttHome: home, output: out}, &stdout1, &stderr1)
	if code1 != 0 {
		t.Fatalf("first build: code=%d stderr=%s", code1, stderr1.String())
	}

	// Discovery now stops returning air-gapped-db (simulating the
	// air-gapped infra not being reachable this run).
	installStubProviderInline(t, home, "kubernetes", `{"components":[{"name":"api","type":"deployment"}]}`)

	// Second build with tombstone: preserves air-gapped-db.
	var stdout2, stderr2 bytes.Buffer
	code2 := runModelBuild(context.Background(), modelBuildFlags{mgttHome: home, output: out, tombstone: []string{"air-gapped-db"}}, &stdout2, &stderr2)
	if code2 != 0 {
		t.Fatalf("second build: code=%d stderr=%s", code2, stderr2.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "air-gapped-db") {
		t.Errorf("tombstoned component should be PRESERVED in output; got: %s", data)
	}
	if !strings.Contains(string(data), "api:") {
		t.Errorf("rediscovered component should be present; got: %s", data)
	}
}

func installStubProviderInline(t *testing.T, home, name, discoverJSON string) {
	t.Helper()
	dir := filepath.Join(home, "providers", name, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"discover\" ]; then\n" +
		"  cat <<'EOF'\n" + discoverJSON + "\nEOF\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "provider"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
