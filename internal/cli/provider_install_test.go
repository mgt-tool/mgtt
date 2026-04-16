package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/registry"
)

// TestInstallProvider_RegistryDisabledWrapsSentinel verifies that the
// CLI-layer error wraps registry.ErrRegistryDisabled so callers can
// errors.Is the sentinel through the full error chain. Walks the path:
// MGTT_REGISTRY_URL=disabled → bare name → no git URL → no local path
// → no local install → registry returns ErrRegistryDisabled → CLI wraps.
func TestInstallProvider_RegistryDisabledWrapsSentinel(t *testing.T) {
	// Isolate MGTT_HOME so we don't accidentally hit a real installed provider.
	t.Setenv("MGTT_HOME", t.TempDir())
	t.Setenv("MGTT_REGISTRY_URL", "disabled")

	var buf bytes.Buffer
	err := installProvider(&buf, "phantom-provider-name-that-cannot-exist")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, registry.ErrRegistryDisabled) {
		t.Fatalf("error must wrap ErrRegistryDisabled (so callers can branch); got %v", err)
	}
	if !strings.Contains(err.Error(), "git URL or local path") {
		t.Fatalf("error should hint at the fix; got %q", err.Error())
	}
}

// TestInstallProvider_RegistryFlagOverridesEnv verifies the flag wins.
// Env says use a (broken) URL; flag says disabled. Flag must win → error
// wraps ErrRegistryDisabled, NOT a network error.
func TestInstallProvider_RegistryFlagOverridesEnv(t *testing.T) {
	t.Setenv("MGTT_HOME", t.TempDir())
	t.Setenv("MGTT_REGISTRY_URL", "https://mirror.invalid/r.yaml")
	prev := providerInstallRegistry
	t.Cleanup(func() { providerInstallRegistry = prev })
	providerInstallRegistry = "disabled"

	var buf bytes.Buffer
	err := installProvider(&buf, "phantom")
	if !errors.Is(err, registry.ErrRegistryDisabled) {
		t.Fatalf("flag must override env; got %v", err)
	}
}

// minimalProviderYAML is a valid provider.yaml for installFromImage tests.
const minimalProviderYAML = `
meta:
  name: test-provider
  version: 1.2.3
  description: a test provider

auth:
  strategy: none
  access:
    probes: none
    writes: none
`

// TestInstallFromImage_WritesFilesAndMeta exercises the full installFromImage
// path with a fake DockerCmd. Verifies that provider.yaml and .mgtt-install.json
// are written to MGTT_HOME/providers/<name>/ with the correct content.
func TestInstallFromImage_WritesFilesAndMeta(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)

	ref := "ghcr.io/example/test-provider:1.2.3@sha256:deadbeefdeadbeefdeadbeefdeadbeef"

	// Fake docker: pull succeeds silently; extract returns minimalProviderYAML.
	fakeDocker := &providersupport.DockerCmd{
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			switch args[0] {
			case "pull":
				return []byte("fake pull output"), nil
			case "run":
				return []byte(minimalProviderYAML), nil
			default:
				t.Errorf("unexpected docker subcommand %q", args[0])
				return nil, nil
			}
		},
	}

	var buf bytes.Buffer
	err := installFromImage(context.Background(), &buf, ref, "", fakeDocker)
	if err != nil {
		t.Fatalf("installFromImage returned error: %v", err)
	}

	destDir := filepath.Join(home, "providers", "test-provider")

	// provider.yaml must exist and contain the manifest bytes.
	yamlPath := filepath.Join(destDir, "provider.yaml")
	yamlBytes, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("provider.yaml not written: %v", err)
	}
	if !strings.Contains(string(yamlBytes), "test-provider") {
		t.Errorf("provider.yaml does not contain provider name; got %q", yamlBytes)
	}

	// .mgtt-install.json must exist with correct method, source, version.
	meta, err := providersupport.ReadInstallMeta(destDir)
	if err != nil {
		t.Fatalf("ReadInstallMeta failed: %v", err)
	}
	if meta.Method != providersupport.InstallMethodImage {
		t.Errorf("expected method=image, got %q", meta.Method)
	}
	if meta.Source != ref {
		t.Errorf("expected source=%q, got %q", ref, meta.Source)
	}
	if meta.Version != "1.2.3" {
		t.Errorf("expected version=1.2.3, got %q", meta.Version)
	}
	if meta.InstalledAt.IsZero() {
		t.Error("InstalledAt must not be zero")
	}

	// stdout must include success line.
	out := buf.String()
	if !strings.Contains(out, "installed test-provider") {
		t.Errorf("expected success message in output; got %q", out)
	}

	// Verify no install hook was run (no hook-related output).
	if strings.Contains(out, "install hook") {
		t.Errorf("image install must not run hooks; got output %q", out)
	}

}

// TestInstallFromImage_NameHintOverride verifies that a positional arg overrides
// the install name from the manifest.
func TestInstallFromImage_NameHintOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)

	ref := "ghcr.io/example/test-provider:1.2.3@sha256:deadbeefdeadbeefdeadbeefdeadbeef"

	fakeDocker := &providersupport.DockerCmd{
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			if args[0] == "run" {
				return []byte(minimalProviderYAML), nil
			}
			return nil, nil
		},
	}

	var buf bytes.Buffer
	err := installFromImage(context.Background(), &buf, ref, "my-override", fakeDocker)
	if err != nil {
		t.Fatalf("installFromImage returned error: %v", err)
	}

	// Provider must be installed under the override name, not the manifest name.
	overrideDir := filepath.Join(home, "providers", "my-override")
	if _, err := os.Stat(filepath.Join(overrideDir, "provider.yaml")); err != nil {
		t.Errorf("provider.yaml not found under override name %q: %v", "my-override", err)
	}
	// Default name dir must NOT exist.
	defaultDir := filepath.Join(home, "providers", "test-provider")
	if _, err := os.Stat(defaultDir); !os.IsNotExist(err) {
		t.Errorf("expected no dir at manifest name %q, but found one", defaultDir)
	}
}

// TestInstallFromImage_RejectsBareTag verifies that refs without @sha256: are rejected.
func TestInstallFromImage_RejectsBareTag(t *testing.T) {
	fakeDocker := &providersupport.DockerCmd{
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			t.Error("docker must not be called for invalid ref")
			return nil, nil
		},
	}
	var buf bytes.Buffer
	err := installFromImage(context.Background(), &buf, "ghcr.io/example/foo:latest", "", fakeDocker)
	if err == nil {
		t.Fatal("expected error for bare tag")
	}
	if !strings.Contains(err.Error(), "sha256") {
		t.Errorf("error should mention sha256; got %q", err.Error())
	}
}
