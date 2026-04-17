package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// TestProviderLs_ShowsInstallMethod verifies that the list output includes
// the install method column (git or image) for each provider.
func TestProviderLs_ShowsInstallMethod(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)

	// Stage a git-installed provider (no .mgtt-install.json, legacy).
	gitDir := filepath.Join(home, "providers", "git-provider")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitYAML := []byte(`meta:
  name: git-provider
  version: 1.0.0
  description: A git-installed provider
auth:
  strategy: none
  access:
    probes: none
    writes: none
`)
	if err := os.WriteFile(filepath.Join(gitDir, "provider.yaml"), gitYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	// Stage an image-installed provider with .mgtt-install.json.
	imageDir := filepath.Join(home, "providers", "image-provider")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	imageYAML := []byte(`meta:
  name: image-provider
  version: 2.0.0
  description: An image-installed provider
auth:
  strategy: none
  access:
    probes: none
    writes: none
`)
	if err := os.WriteFile(filepath.Join(imageDir, "provider.yaml"), imageYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write .mgtt-install.json for image provider.
	imageMeta := providersupport.InstallMeta{
		Method:  providersupport.InstallMethodImage,
		Source:  "ghcr.io/example/image-provider:2.0.0@sha256:deadbeef",
		Version: "2.0.0",
	}
	if err := providersupport.WriteInstallMeta(imageDir, imageMeta); err != nil {
		t.Fatal(err)
	}

	// Load providers the same way the CLI does.
	names := providersupport.ListEmbedded()
	var providers []*providersupport.Provider
	for _, name := range names {
		p, err := providersupport.LoadEmbedded(name)
		if err != nil {
			t.Fatalf("failed to load provider %q: %v", name, err)
		}
		providers = append(providers, p)
	}

	// Render the output.
	var buf bytes.Buffer
	renderProviderLs(&buf, providers)
	output := buf.String()

	// Verify git-provider line contains "git" in the method column.
	if !strings.Contains(output, "git-provider") {
		t.Fatalf("output should contain git-provider; got:\n%s", output)
	}
	// Find the line with git-provider and verify it contains "git".
	gitLine := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "git-provider") {
			gitLine = line
			break
		}
	}
	if gitLine == "" {
		t.Fatalf("no line found for git-provider; output:\n%s", output)
	}
	if !strings.Contains(gitLine, "git") {
		t.Errorf("git-provider line should contain 'git'; got:\n%s", gitLine)
	}

	// Verify image-provider line contains "image" in the method column.
	if !strings.Contains(output, "image-provider") {
		t.Fatalf("output should contain image-provider; got:\n%s", output)
	}
	// Find the line with image-provider and verify it contains "image".
	imageLine := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "image-provider") {
			imageLine = line
			break
		}
	}
	if imageLine == "" {
		t.Fatalf("no line found for image-provider; output:\n%s", output)
	}
	if !strings.Contains(imageLine, "image") {
		t.Errorf("image-provider line should contain 'image'; got:\n%s", imageLine)
	}

	// Verify column alignment: both lines should have their description text
	// starting at the same column position.
	gitDescStart := strings.Index(gitLine, "A git-installed")
	imageDescStart := strings.Index(imageLine, "An image-installed")
	if gitDescStart != imageDescStart {
		t.Errorf("description columns misaligned: git at col %d, image at col %d\ngit:   %s\nimage: %s",
			gitDescStart, imageDescStart, gitLine, imageLine)
	}
}

// TestProviderLs_EmptyList verifies that an empty provider list renders
// the "no providers installed" message.
func TestProviderLs_EmptyList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)

	var providers []*providersupport.Provider
	var buf bytes.Buffer
	renderProviderLs(&buf, providers)
	output := buf.String()

	if !strings.Contains(output, "no providers installed") {
		t.Errorf("expected 'no providers installed' message; got: %s", output)
	}
}

// TestProviderLs_CorruptMetaShowsQuestion verifies that if .mgtt-install.json
// is corrupt or unreadable, the method column shows "?" and listing continues.
func TestProviderLs_CorruptMetaShowsQuestion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)

	// Stage a provider with corrupt .mgtt-install.json.
	corruptDir := filepath.Join(home, "providers", "corrupt-provider")
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlFile := filepath.Join(corruptDir, "provider.yaml")
	yamlContent := []byte(`meta:
  name: corrupt-provider
  version: 1.5.0
  description: Provider with corrupt metadata
auth:
  strategy: none
  access:
    probes: none
    writes: none
`)
	if err := os.WriteFile(yamlFile, yamlContent, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write corrupt JSON to .mgtt-install.json.
	metaFile := filepath.Join(corruptDir, ".mgtt-install.json")
	if err := os.WriteFile(metaFile, []byte("not: [valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Load and render.
	names := providersupport.ListEmbedded()
	var providers []*providersupport.Provider
	for _, name := range names {
		p, err := providersupport.LoadEmbedded(name)
		if err != nil {
			t.Fatalf("failed to load provider %q: %v", name, err)
		}
		providers = append(providers, p)
	}

	var buf bytes.Buffer
	renderProviderLs(&buf, providers)
	output := buf.String()

	// Verify corrupt-provider line contains "?" in the method column.
	corruptLine := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "corrupt-provider") {
			corruptLine = line
			break
		}
	}
	if corruptLine == "" {
		t.Fatalf("no line found for corrupt-provider; output:\n%s", output)
	}
	if !strings.Contains(corruptLine, "?") {
		t.Errorf("corrupt-provider line should contain '?' for unparseable metadata; got:\n%s", corruptLine)
	}
}

// TestProviderLs_ShowsCapabilities verifies that image.needs declared in
// provider.yaml surfaces as a [...] column in the list output, so
// operators see the runtime scope alongside the install method.
func TestProviderLs_ShowsCapabilities(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)

	// Provider that declares caps.
	capDir := filepath.Join(home, "providers", "capful")
	if err := os.MkdirAll(capDir, 0o755); err != nil {
		t.Fatal(err)
	}
	capYAML := []byte(`meta:
  name: capful
  version: 1.0.0
  description: uses host resources
  command: /bin/capful
auth:
  strategy: none
  access: {probes: none, writes: none}

needs: [kubectl]
network: host
`)
	if err := os.WriteFile(filepath.Join(capDir, "provider.yaml"), capYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	// Provider that declares no caps (omit the whole block).
	plainDir := filepath.Join(home, "providers", "plain")
	if err := os.MkdirAll(plainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	plainYAML := []byte(`meta:
  name: plain
  version: 1.0.0
  description: no caps needed
auth:
  strategy: none
  access: {probes: none, writes: none}
`)
	if err := os.WriteFile(filepath.Join(plainDir, "provider.yaml"), plainYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	names := providersupport.ListEmbedded()
	var providers []*providersupport.Provider
	for _, name := range names {
		p, err := providersupport.LoadEmbedded(name)
		if err != nil {
			t.Fatal(err)
		}
		providers = append(providers, p)
	}

	var buf bytes.Buffer
	renderProviderLs(&buf, providers)
	output := buf.String()

	// capful row must contain the bracketed cap list.
	var capfulLine, plainLine string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "capful") {
			capfulLine = line
		}
		if strings.Contains(line, "plain") {
			plainLine = line
		}
	}
	if capfulLine == "" || plainLine == "" {
		t.Fatalf("both providers must render; got:\n%s", output)
	}
	if !strings.Contains(capfulLine, "[kubectl]") {
		t.Errorf("capful line must show bracketed caps; got:\n%s", capfulLine)
	}
	if !strings.Contains(plainLine, "-") {
		t.Errorf("plain line must show '-' for no caps; got:\n%s", plainLine)
	}
	// Both lines must be column-aligned on the description.
	capfulDesc := strings.Index(capfulLine, "uses host resources")
	plainDesc := strings.Index(plainLine, "no caps needed")
	if capfulDesc != plainDesc {
		t.Errorf("description columns misaligned: capful col %d, plain col %d\n%s\n%s",
			capfulDesc, plainDesc, capfulLine, plainLine)
	}
}
