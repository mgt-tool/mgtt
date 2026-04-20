package providersupport

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// installStubProvider creates a minimal provider install at
// <home>/providers/<name>/ with a bin/provider shell script returning
// the given JSON from `discover`.
func installStubProvider(t *testing.T, home, name, discoverJSON string) {
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
	path := filepath.Join(dir, "provider")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// installNonDiscoverableProvider installs a provider whose
// `discover` subcommand exits non-zero — simulating an older provider
// that doesn't support discovery.
func installNonDiscoverableProvider(t *testing.T, home, name string) {
	t.Helper()
	dir := filepath.Join(home, "providers", name, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "provider"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverAll_HappyPath(t *testing.T) {
	home := t.TempDir()
	installStubProvider(t, home, "kubernetes", `{"components":[{"name":"api","type":"deployment"}]}`)
	installStubProvider(t, home, "aws", `{"components":[{"name":"rds","type":"rds_instance"}]}`)

	results, failures := DiscoverAll(context.Background(), home)
	if len(failures) != 0 {
		t.Errorf("unexpected failures: %v", failures)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d: %+v", len(results), results)
	}
	if results["kubernetes"].Components[0].Name != "api" {
		t.Errorf("kubernetes result wrong: %+v", results["kubernetes"])
	}
	if results["aws"].Components[0].Name != "rds" {
		t.Errorf("aws result wrong: %+v", results["aws"])
	}
}

func TestDiscoverAll_SkipsNonDiscoverable(t *testing.T) {
	home := t.TempDir()
	installStubProvider(t, home, "kubernetes", `{"components":[{"name":"api","type":"deployment"}]}`)
	installNonDiscoverableProvider(t, home, "legacy")

	results, failures := DiscoverAll(context.Background(), home)
	if _, ok := results["kubernetes"]; !ok {
		t.Error("kubernetes should be in results")
	}
	if _, ok := results["legacy"]; ok {
		t.Error("legacy should NOT be in results (failed discover)")
	}
	if _, ok := failures["legacy"]; !ok {
		t.Error("legacy should be recorded in failures")
	}
}

func TestDiscoverAll_EmptyHome(t *testing.T) {
	home := t.TempDir()
	results, failures := DiscoverAll(context.Background(), home)
	if len(results) != 0 || len(failures) != 0 {
		t.Errorf("want empty results and failures; got %v / %v", results, failures)
	}
}
