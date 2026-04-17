package cli

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/registry"
)

// tarManifest returns the bytes `docker cp cid:/manifest.yaml -` would emit:
// a tar archive containing a single regular-file entry for manifest.yaml.
func tarManifest(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "manifest.yaml",
		Mode:     0o644,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

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

// minimalProviderYAML is a valid manifest.yaml for installFromImage tests.
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
// path with a fake DockerCmd. Verifies that manifest.yaml and .mgtt-install.json
// are written to MGTT_HOME/providers/<name>/ with the correct content.
func TestInstallFromImage_WritesFilesAndMeta(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)

	ref := "ghcr.io/example/test-provider:1.2.3@sha256:deadbeefdeadbeefdeadbeefdeadbeef"

	// Fake docker: pull → create → cp (returns tar stream) → rm.
	fakeDocker := &providersupport.DockerCmd{
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			switch args[0] {
			case "pull":
				return []byte("fake pull output"), nil
			case "create":
				return []byte("cid-test\n"), nil
			case "cp":
				return tarManifest(t, minimalProviderYAML), nil
			case "rm":
				return nil, nil
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

	// manifest.yaml must exist and contain the manifest bytes.
	yamlPath := filepath.Join(destDir, "manifest.yaml")
	yamlBytes, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("manifest.yaml not written: %v", err)
	}
	if !strings.Contains(string(yamlBytes), "test-provider") {
		t.Errorf("manifest.yaml does not contain provider name; got %q", yamlBytes)
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

// TestInstallFromImage_WritesTypesDir verifies that a multi-file provider
// image (kubernetes-style /types/*.yaml) lands under destDir/types/ after
// install. Prior to this, only /manifest.yaml was extracted and all types
// silently vanished.
func TestInstallFromImage_WritesTypesDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)

	ref := "ghcr.io/example/multi-type:1.0.0@sha256:deadbeefdeadbeefdeadbeefdeadbeef"

	// Build a tar shaped like `docker cp cid:/types -`: a dir entry plus
	// two regular-file entries.
	var typesTar bytes.Buffer
	tw := tar.NewWriter(&typesTar)
	_ = tw.WriteHeader(&tar.Header{Name: "types/", Mode: 0o755, Typeflag: tar.TypeDir})
	for name, body := range map[string]string{
		"deployment.yaml": "description: k8s deployment\n",
		"service.yaml":    "description: k8s service\n",
	} {
		_ = tw.WriteHeader(&tar.Header{
			Name:     "types/" + name,
			Mode:     0o644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()

	cpCalls := 0
	fakeDocker := &providersupport.DockerCmd{
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			switch args[0] {
			case "pull":
				return nil, nil
			case "create":
				return []byte("cid-test\n"), nil
			case "cp":
				cpCalls++
				joined := strings.Join(args, " ")
				switch {
				case strings.Contains(joined, ":/manifest.yaml"):
					return tarManifest(t, minimalProviderYAML), nil
				case strings.Contains(joined, ":/types"):
					return typesTar.Bytes(), nil
				}
			case "rm":
				return nil, nil
			}
			return nil, nil
		},
	}

	var buf bytes.Buffer
	if err := installFromImage(context.Background(), &buf, ref, "", fakeDocker); err != nil {
		t.Fatalf("installFromImage: %v", err)
	}

	typesDir := filepath.Join(home, "providers", "test-provider", "types")
	for _, fname := range []string{"deployment.yaml", "service.yaml"} {
		if _, err := os.Stat(filepath.Join(typesDir, fname)); err != nil {
			t.Errorf("types file %s missing after image install: %v", fname, err)
		}
	}
	// cp must have been invoked for both /manifest.yaml and /types.
	if cpCalls != 2 {
		t.Errorf("expected 2 cp calls (manifest + types), got %d", cpCalls)
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
			switch args[0] {
			case "create":
				return []byte("cid-test\n"), nil
			case "cp":
				return tarManifest(t, minimalProviderYAML), nil
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
	if _, err := os.Stat(filepath.Join(overrideDir, "manifest.yaml")); err != nil {
		t.Errorf("manifest.yaml not found under override name %q: %v", "my-override", err)
	}
	// Default name dir must NOT exist.
	defaultDir := filepath.Join(home, "providers", "test-provider")
	if _, err := os.Stat(defaultDir); !os.IsNotExist(err) {
		t.Errorf("expected no dir at manifest name %q, but found one", defaultDir)
	}
}

// TestInstallFromImage_RejectsMalformedManifest verifies that a malformed
// manifest.yaml extracted from an image is rejected before any files are written.
func TestInstallFromImage_RejectsMalformedManifest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MGTT_HOME", root)

	fakeDocker := &providersupport.DockerCmd{
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			switch args[0] {
			case "create":
				return []byte("cid-test\n"), nil
			case "cp":
				// extract returns garbage YAML (wrapped in a valid tar entry).
				return tarManifest(t, "not: [valid yaml"), nil
			}
			return nil, nil
		},
	}

	var buf bytes.Buffer
	err := installFromImage(
		context.Background(),
		&buf,
		"ghcr.io/x/provider@sha256:deadbeefdeadbeefdeadbeefdeadbeef",
		"",
		fakeDocker,
	)
	if err == nil {
		t.Fatal("expected error on malformed manifest, got nil")
	}
	// Error should mention parsing.
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parse failure; got %v", err)
	}
	// No install dir should have been created.
	if entries, _ := os.ReadDir(filepath.Join(root, "providers")); len(entries) != 0 {
		t.Errorf("no dir should be created on manifest parse failure; got %d entries", len(entries))
	}
}

// TestInstallFromImage_PullFailurePropagates verifies that a docker pull failure
// is returned immediately and no install directory is created.
func TestInstallFromImage_PullFailurePropagates(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MGTT_HOME", root)

	fakeDocker := &providersupport.DockerCmd{
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			if args[0] == "pull" {
				return []byte("Error response from daemon: unauthorized"), fmt.Errorf("exit status 1")
			}
			t.Fatalf("extract should not be called after pull failure; got args %v", args)
			return nil, nil
		},
	}

	var buf bytes.Buffer
	err := installFromImage(
		context.Background(),
		&buf,
		"ghcr.io/private/provider@sha256:deadbeefdeadbeefdeadbeefdeadbeef",
		"",
		fakeDocker,
	)
	if err == nil {
		t.Fatal("expected error on pull failure, got nil")
	}
	// No install dir should have been created.
	if entries, _ := os.ReadDir(filepath.Join(root, "providers")); len(entries) != 0 {
		t.Errorf("no dir should be created on pull failure; got %d entries", len(entries))
	}
}

// TestDeriveNamespace covers all supported input forms for deriveNamespace.
func TestDeriveNamespace(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Image refs with digest
		{
			input: "ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0@sha256:abc123",
			want:  "mgt-tool",
		},
		{
			input: "ghcr.io/mgt-tool/mgtt-provider-kubernetes:1.0.0@sha256:deadbeef",
			want:  "mgt-tool",
		},
		// Image ref with only digest (no tag)
		{
			input: "ghcr.io/mgt-tool/mgtt-provider-docker@sha256:deadbeef",
			want:  "mgt-tool",
		},
		// Git HTTPS URLs
		{
			input: "https://github.com/mgt-tool/mgtt-provider-tempo",
			want:  "mgt-tool",
		},
		{
			input: "https://github.com/org-name/some-provider",
			want:  "org-name",
		},
		// Git SSH URL
		{
			input: "git@github.com:mgt-tool/mgtt-provider-tempo.git",
			want:  "mgt-tool",
		},
		// HTTP URL
		{
			input: "http://internal.host/myorg/myprovider",
			want:  "myorg",
		},
		// Bare name — no namespace
		{
			input: "kubernetes",
			want:  "",
		},
		// Registry with no path segments after host — no namespace
		{
			input: "ghcr.io/standalone",
			want:  "",
		},
		// Empty string
		{
			input: "",
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := deriveNamespace(tc.input)
			if got != tc.want {
				t.Errorf("deriveNamespace(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
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

func TestInstallFromImage_PrintsDeclaredCaps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)

	ref := "ghcr.io/example/capped:1.0.0@sha256:deadbeefdeadbeefdeadbeefdeadbeef"

	const capsProviderYAML = `
meta:
  name: capped
  version: 1.0.0
  command: /bin/provider
auth:
  strategy: none
  access: {probes: none, writes: none}

needs: [kubectl]
network: host
`
	fakeDocker := &providersupport.DockerCmd{
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			switch args[0] {
			case "pull":
				return nil, nil
			case "create":
				return []byte("cid-test\n"), nil
			case "cp":
				joined := strings.Join(args, " ")
				if strings.Contains(joined, ":/manifest.yaml") {
					return tarManifest(t, capsProviderYAML), nil
				}
				// /types missing → no types. DockerCmd.ExtractTypes treats
				// cp-error as "no types"; we return an error to exercise that.
				return nil, errors.New("no such path")
			case "rm":
				return nil, nil
			}
			return nil, nil
		},
	}

	var buf bytes.Buffer
	if err := installFromImage(context.Background(), &buf, ref, "", fakeDocker); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "capabilities: kubectl") {
		t.Errorf("install must print declared caps; got %q", out)
	}
	if !strings.Contains(out, "network: host") {
		t.Errorf("install must print non-default network mode; got %q", out)
	}
}

func TestInstallFromImage_RejectsUnknownCap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)

	ref := "ghcr.io/example/bogus:1.0.0@sha256:deadbeefdeadbeefdeadbeefdeadbeef"

	const unknownCapYAML = `
meta:
  name: bogus
  version: 1.0.0
  command: /bin/provider
auth:
  strategy: none
  access: {probes: none, writes: none}

needs: [vault-nope]
`
	fakeDocker := &providersupport.DockerCmd{
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			switch args[0] {
			case "pull":
				return nil, nil
			case "create":
				return []byte("cid-test\n"), nil
			case "cp":
				if strings.Contains(strings.Join(args, " "), ":/manifest.yaml") {
					return tarManifest(t, unknownCapYAML), nil
				}
				return nil, errors.New("no /types")
			case "rm":
				return nil, nil
			}
			return nil, nil
		},
	}

	var buf bytes.Buffer
	err := installFromImage(context.Background(), &buf, ref, "", fakeDocker)
	if err == nil {
		t.Fatal("expected install to fail on unknown capability")
	}
	if !strings.Contains(err.Error(), "vault-nope") {
		t.Errorf("error must name the unknown capability; got %v", err)
	}
	// No dir should have been created.
	if _, statErr := os.Stat(filepath.Join(home, "providers", "bogus")); !os.IsNotExist(statErr) {
		t.Errorf("no install dir should exist after cap-validation failure; stat err=%v", statErr)
	}
}
