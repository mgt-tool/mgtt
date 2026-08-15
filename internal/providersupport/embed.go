// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package providersupport

import (
	"fmt"
	"os"
	"path/filepath"
)

// Home returns the mgtt home directory. Honors MGTT_HOME when set, else
// falls back to ~/.mgtt. This is the single source of truth for home
// resolution — InstallRoot, SearchDirs, and CLI consumers all route
// through here.
func Home() (string, error) {
	if home := os.Getenv("MGTT_HOME"); home != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mgtt"), nil
}

// InstallRoot returns the directory where `mgtt provider install` writes
// newly-installed providers. Honors MGTT_HOME when set (matching the
// precedence used by SearchDirs), else falls back to ~/.mgtt/providers.
// This is the single source of truth for the install-write path; callers
// that only READ providers should use SearchDirs or ProviderDir instead.
func InstallRoot() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "providers"), nil
}

// SearchDirs returns the provider search directories in priority order:
//  1. $MGTT_HOME/providers/
//  2. ~/.mgtt/providers/
//  3. ./providers/
//
// The MGTT_HOME-first entry and the ~/.mgtt fallback both live under
// Home() — so when MGTT_HOME is set and resolves to ~/.mgtt the list
// collapses to two unique entries, and when MGTT_HOME points elsewhere
// both are scanned in order.
func SearchDirs() []string {
	var dirs []string
	if home := os.Getenv("MGTT_HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, "providers"))
	}
	if homeDir, err := os.UserHomeDir(); err == nil {
		fallback := filepath.Join(homeDir, ".mgtt", "providers")
		if len(dirs) == 0 || dirs[0] != fallback {
			dirs = append(dirs, fallback)
		}
	}
	dirs = append(dirs, "providers")
	return dirs
}

// ProviderDir returns the directory for a named provider, or "" if not found.
func ProviderDir(name string) string {
	for _, dir := range SearchDirs() {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(filepath.Join(candidate, "manifest.yaml")); err == nil {
			return candidate
		}
	}
	return ""
}

// LoadEmbedded loads a provider by name from the search path. Uses
// LoadFromDir so multi-file providers (types/*.yaml) are supported; inline
// types in manifest.yaml still work via LoadFromDir's fallback.
func LoadEmbedded(name string) (*Provider, error) {
	dir := ProviderDir(name)
	if dir == "" {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	return LoadFromDir(dir)
}

// ListEmbedded returns the names of all providers found across all search paths.
func ListEmbedded() []string {
	seen := map[string]bool{}
	var names []string
	for _, dir := range SearchDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && !seen[e.Name()] {
				if _, err := os.Stat(filepath.Join(dir, e.Name(), "manifest.yaml")); err == nil {
					seen[e.Name()] = true
					names = append(names, e.Name())
				}
			}
		}
	}
	return names
}

// LoadAllEmbedded returns a registry populated with every provider
// discovered by ListEmbedded, without enforcing meta.requires.mgtt —
// used by uninstall and ls where incompatible providers must still be
// visible. Providers whose meta.name claims the reserved generic
// identity are skipped; the second return value reports those so the
// CLI can error at the boundary instead of shadowing the built-in.
func LoadAllEmbedded() (*Registry, []string) {
	return loadAll(nil)
}

// LoadAllForUse mirrors LoadAllEmbedded but filters out providers that
// fail CheckCompatible. Used by every non-uninstall/ls caller so
// incompatible plugins don't drive probes against a stale protocol.
func LoadAllForUse() (*Registry, []string) {
	return loadAll(func(p *Provider) error { return p.CheckCompatible() })
}

// loadAll is the shared body behind LoadAll{Embedded,ForUse}: discover,
// optionally filter via keep(), skip reserved-name collisions. keep may
// be nil when no compatibility filtering is wanted.
func loadAll(keep func(*Provider) error) (*Registry, []string) {
	reg := NewRegistry()
	var reserved []string
	for _, name := range ListEmbedded() {
		p, err := LoadEmbedded(name)
		if err != nil {
			continue
		}
		if keep != nil {
			if err := keep(p); err != nil {
				continue
			}
		}
		if IsGenericName(p.Meta.Name) {
			reserved = append(reserved, name)
			continue
		}
		reg.Register(p)
	}
	return reg, reserved
}

// LoadForUse is the version-gated loader every non-uninstall/ls caller MUST
// use. It loads the provider and runs CheckCompatible; an incompatible
// provider returns an error so callers don't evaluate stale protocol-format
// expressions or invoke an incompatible runner.
func LoadForUse(name string) (*Provider, error) {
	p, err := LoadEmbedded(name)
	if err != nil {
		return nil, err
	}
	if err := p.CheckCompatible(); err != nil {
		return nil, fmt.Errorf("provider %q: %w", name, err)
	}
	return p, nil
}
