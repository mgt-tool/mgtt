package providersupport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstallMeta_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := InstallMeta{
		Method:      InstallMethodImage,
		Source:      "ghcr.io/foo/bar@sha256:abc",
		InstalledAt: time.Now().UTC().Truncate(time.Second),
		Version:     "0.2.0",
	}
	if err := WriteInstallMeta(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadInstallMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != want.Method || got.Source != want.Source ||
		got.Version != want.Version || !got.InstalledAt.Equal(want.InstalledAt) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestReadInstallMeta_AbsentReturnsGitDefault(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadInstallMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Backward compat: existing installs (no .mgtt-install.json) are git-installed.
	if got.Method != InstallMethodGit {
		t.Fatalf("expected default method 'git' for legacy installs, got %q", got.Method)
	}
}

func TestReadInstallMeta_CorruptFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".mgtt-install.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadInstallMeta(dir)
	if err == nil {
		t.Fatal("expected error on corrupt metadata, got nil")
	}
}

// TestInstallMeta_NamespaceBackwardCompat ensures that a JSON blob written
// before the Namespace field was added (i.e. without a "namespace" key) still
// decodes cleanly with Namespace == "".  This pins Go's zero-value semantics
// so a future refactor cannot silently break old install records.
func TestInstallMeta_NamespaceBackwardCompat(t *testing.T) {
	const legacy = `{
		"method": "git",
		"source": "https://github.com/mgt-tool/mgtt-provider-kubernetes",
		"installed_at": "2024-01-01T00:00:00Z",
		"version": "0.1.0"
	}`
	var m InstallMeta
	if err := json.Unmarshal([]byte(legacy), &m); err != nil {
		t.Fatalf("unmarshal legacy JSON: %v", err)
	}
	if m.Namespace != "" {
		t.Fatalf("expected Namespace to be empty for legacy records, got %q", m.Namespace)
	}
	if m.Method != InstallMethodGit {
		t.Fatalf("expected method 'git', got %q", m.Method)
	}
}
