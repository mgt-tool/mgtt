package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFetch_DisabledSentinel(t *testing.T) {
	for _, val := range []string{"disabled", "none", "off", "DISABLED", " none "} {
		t.Run(val, func(t *testing.T) {
			t.Setenv("MGTT_REGISTRY_URL", val)
			_, err := Fetch(Source{})
			if !errors.Is(err, ErrRegistryDisabled) {
				t.Fatalf("want ErrRegistryDisabled, got %v", err)
			}
		})
	}
}

func TestFetch_DisabledViaExplicitOverride(t *testing.T) {
	// Env var unset; explicit override wins.
	t.Setenv("MGTT_REGISTRY_URL", "")
	_, err := Fetch(Source{URL: "disabled"})
	if !errors.Is(err, ErrRegistryDisabled) {
		t.Fatalf("want ErrRegistryDisabled, got %v", err)
	}
}

func TestFetch_OverrideBeatsEnv(t *testing.T) {
	// Env says use the default community URL; flag override says disabled.
	// Flag wins.
	t.Setenv("MGTT_REGISTRY_URL", "https://example.invalid/registry.yaml")
	_, err := Fetch(Source{URL: "disabled"})
	if !errors.Is(err, ErrRegistryDisabled) {
		t.Fatalf("flag should beat env; got %v", err)
	}
}

func TestFetch_FileURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.yaml")
	body := []byte(`providers:
  alpha:
    url: https://example.com/mgtt-provider-alpha
    description: a fictional alpha provider
    tags: [example]
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MGTT_REGISTRY_URL", "file://"+path)
	reg, err := Fetch(Source{})
	if err != nil {
		t.Fatalf("fetch file://: %v", err)
	}
	e, ok := reg.Lookup("alpha")
	if !ok {
		t.Fatal("alpha entry missing")
	}
	if e.URL != "https://example.com/mgtt-provider-alpha" {
		t.Fatalf("got URL %q", e.URL)
	}
}

func TestFetch_FileURL_NotFound(t *testing.T) {
	t.Setenv("MGTT_REGISTRY_URL", "file:///nonexistent/path/does-not-exist.yaml")
	_, err := Fetch(Source{})
	if err == nil || !strings.Contains(err.Error(), "read registry file") {
		t.Fatalf("expected file-read error, got %v", err)
	}
}

func TestFetch_FileURL_BadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("not: [valid yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MGTT_REGISTRY_URL", "file://"+path)
	_, err := Fetch(Source{})
	if err == nil || !strings.Contains(err.Error(), "parse registry file") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestCacheFilePath_KeyedOnURL(t *testing.T) {
	// Distinct URLs must produce distinct cache paths so switching
	// MGTT_REGISTRY_URL doesn't serve content fetched against another
	// identity to the new one.
	t.Setenv("MGTT_HOME", t.TempDir())
	a := cacheFilePath("https://mirror1.corp/registry.yaml")
	b := cacheFilePath("https://mirror2.corp/registry.yaml")
	c := cacheFilePath("https://mgt-tool.github.io/mgtt/registry.yaml")
	if a == b || a == c || b == c {
		t.Fatalf("cache paths must differ per URL: %q %q %q", a, b, c)
	}
	// Same URL → same path (deterministic).
	if a != cacheFilePath("https://mirror1.corp/registry.yaml") {
		t.Fatal("cache path must be deterministic for the same URL")
	}
}

func TestCacheFilePath_HonorsMgttHome(t *testing.T) {
	t.Setenv("MGTT_HOME", "/opt/mgtt")
	got := cacheFilePath("https://example.com/r.yaml")
	if got == "" || got[:len("/opt/mgtt/cache/registry/")] != "/opt/mgtt/cache/registry/" {
		t.Fatalf("MGTT_HOME not honored, got %q", got)
	}
}

func TestFileURLPath(t *testing.T) {
	cases := map[string]string{
		"file:///opt/r.yaml":             "/opt/r.yaml",
		"file://localhost/opt/r.yaml":    "/opt/r.yaml",
		"file:///opt/my%20registry.yaml": "/opt/my registry.yaml",
	}
	for in, want := range cases {
		got, err := fileURLPath(in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestFileURLPath_RejectsForeignHost(t *testing.T) {
	_, err := fileURLPath("file://other-host/opt/r.yaml")
	if err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("expected host-rejection error, got %v", err)
	}
}

func TestResolveURL_Priority(t *testing.T) {
	// Explicit > env > default.
	t.Setenv("MGTT_REGISTRY_URL", "from-env")
	if got := resolveURL("from-flag"); got != "from-flag" {
		t.Errorf("flag should win, got %q", got)
	}
	if got := resolveURL(""); got != "from-env" {
		t.Errorf("env fallback failed, got %q", got)
	}
	t.Setenv("MGTT_REGISTRY_URL", "")
	if got := resolveURL(""); got != DefaultRegistryURL {
		t.Errorf("default fallback failed, got %q", got)
	}
}

func TestEntry_DecodesOptionalImage(t *testing.T) {
	data := `providers:
  foo:
    url: https://github.com/example/foo
    image: ghcr.io/example/foo@sha256:abc
    description: test
`
	var reg Registry
	err := yaml.Unmarshal([]byte(data), &reg)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := reg.Providers["foo"]
	if got.URL != "https://github.com/example/foo" {
		t.Errorf("URL: got %q", got.URL)
	}
	if got.Image != "ghcr.io/example/foo@sha256:abc" {
		t.Errorf("Image: got %q", got.Image)
	}
}

func TestEntry_ImageIsOptional(t *testing.T) {
	data := `providers:
  foo:
    url: https://github.com/example/foo
    description: test
`
	var reg Registry
	err := yaml.Unmarshal([]byte(data), &reg)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if reg.Providers["foo"].Image != "" {
		t.Errorf("expected empty Image when omitted, got %q", reg.Providers["foo"].Image)
	}
}
