package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

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
