package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallProvider_NotInstalled(t *testing.T) {
	var buf bytes.Buffer
	err := uninstallProvider(&buf, "does-not-exist-anywhere")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected 'not installed' error, got: %v", err)
	}
}

func TestUninstallProvider_RemovesDirectory(t *testing.T) {
	// Stage a minimal provider in a temp search path.
	home := t.TempDir()
	dir := filepath.Join(home, "providers", "testprov")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := []byte("meta:\n  name: testprov\n  version: 1.0.0\n")
	if err := os.WriteFile(filepath.Join(dir, "provider.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	// Point MGTT_HOME at the temp dir so ProviderDir finds it.
	t.Setenv("MGTT_HOME", home)

	var buf bytes.Buffer
	if err := uninstallProvider(&buf, "testprov"); err != nil {
		t.Fatalf("uninstall: %v\noutput: %s", err, buf.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory should be gone, still exists")
	}
	if !strings.Contains(buf.String(), "uninstalled testprov") {
		t.Fatalf("output should confirm uninstall: %s", buf.String())
	}
}

func TestUninstallProvider_RunsHook(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "providers", "hookprov")
	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := []byte("meta:\n  name: hookprov\n  version: 1.0.0\nhooks:\n  uninstall: hooks/uninstall.sh\n")
	if err := os.WriteFile(filepath.Join(dir, "provider.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}
	// Hook writes a marker file so we can verify it ran.
	marker := filepath.Join(home, "hook-ran")
	hook := []byte("#!/bin/bash\ntouch " + marker + "\n")
	if err := os.WriteFile(filepath.Join(hooksDir, "uninstall.sh"), hook, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MGTT_HOME", home)

	var buf bytes.Buffer
	if err := uninstallProvider(&buf, "hookprov"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Fatal("uninstall hook did not run (marker file missing)")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("directory should be removed after hook")
	}
}

func TestUninstallProvider_BrokenYAMLStillRemovesDir(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "providers", "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provider.yaml"), []byte("not: [valid yaml"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MGTT_HOME", home)

	var buf bytes.Buffer
	if err := uninstallProvider(&buf, "broken"); err != nil {
		t.Fatalf("broken yaml should not prevent uninstall: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("directory should be gone even with broken YAML")
	}
}

func TestUninstallProvider_ImageInstall_SkipsHook(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "providers", "imageprov")
	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Provider YAML declares an uninstall hook.
	yaml := []byte("meta:\n  name: imageprov\n  version: 1.0.0\nhooks:\n  uninstall: hooks/uninstall.sh\n")
	if err := os.WriteFile(filepath.Join(dir, "provider.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write an uninstall hook that creates a marker file if executed.
	marker := filepath.Join(home, "hook-ran-marker")
	hook := []byte("#!/bin/bash\ntouch " + marker + "\n")
	if err := os.WriteFile(filepath.Join(hooksDir, "uninstall.sh"), hook, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write .mgtt-install.json indicating this is an image install.
	installMeta := []byte(`{
  "method": "image",
  "source": "ghcr.io/test@sha256:abc123",
  "installed_at": "2025-01-01T00:00:00Z"
}`)
	if err := os.WriteFile(filepath.Join(dir, ".mgtt-install.json"), installMeta, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MGTT_HOME", home)

	var buf bytes.Buffer
	if err := uninstallProvider(&buf, "imageprov"); err != nil {
		t.Fatalf("uninstall: %v\noutput: %s", err, buf.String())
	}

	// Assert hook was NOT run.
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("hook should not have run for image-installed provider (marker file exists)")
	}

	// Assert directory was removed.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("directory should be removed")
	}

	// Assert docker rmi hint is in output.
	output := buf.String()
	if !strings.Contains(output, "docker rmi ghcr.io/test@sha256:abc123") {
		t.Fatalf("output should contain docker rmi hint, got: %s", output)
	}
}

func TestUninstallProvider_GitInstall_RunsHookAsBefore(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "providers", "gitprov")
	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Provider YAML declares an uninstall hook.
	yaml := []byte("meta:\n  name: gitprov\n  version: 1.0.0\nhooks:\n  uninstall: hooks/uninstall.sh\n")
	if err := os.WriteFile(filepath.Join(dir, "provider.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write an uninstall hook that creates a marker file when executed.
	marker := filepath.Join(home, "git-hook-ran-marker")
	hook := []byte("#!/bin/bash\ntouch " + marker + "\n")
	if err := os.WriteFile(filepath.Join(hooksDir, "uninstall.sh"), hook, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write .mgtt-install.json indicating this is a git install.
	installMeta := []byte(`{
  "method": "git",
  "source": "https://github.com/example/provider.git",
  "installed_at": "2025-01-01T00:00:00Z"
}`)
	if err := os.WriteFile(filepath.Join(dir, ".mgtt-install.json"), installMeta, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MGTT_HOME", home)

	var buf bytes.Buffer
	if err := uninstallProvider(&buf, "gitprov"); err != nil {
		t.Fatalf("uninstall: %v\noutput: %s", err, buf.String())
	}

	// Assert hook WAS run.
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Fatal("hook should have run for git-installed provider (marker file missing)")
	}

	// Assert directory was removed.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("directory should be removed")
	}

	// Assert docker rmi hint is NOT in output.
	output := buf.String()
	if strings.Contains(output, "docker rmi") {
		t.Fatalf("output should not contain docker rmi for git install, got: %s", output)
	}
}
