package providersupport

import (
	"fmt"
	"os"
	"path/filepath"
)

// InstallRoot returns the directory where `mgtt provider install` writes
// newly-installed providers. Honors MGTT_HOME when set (matching the
// precedence used by SearchDirs), else falls back to ~/.mgtt/providers.
// This is the single source of truth for the install-write path; callers
// that only READ providers should use SearchDirs or ProviderDir instead.
func InstallRoot() (string, error) {
	if home := os.Getenv("MGTT_HOME"); home != "" {
		return filepath.Join(home, "providers"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mgtt", "providers"), nil
}

// SearchDirs returns the provider search directories in priority order:
//  1. $MGTT_HOME/providers/
//  2. ~/.mgtt/providers/
//  3. ./providers/
func SearchDirs() []string {
	var dirs []string
	if home := os.Getenv("MGTT_HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, "providers"))
	}
	if homeDir, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(homeDir, ".mgtt", "providers"))
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

// LoadAllEmbedded returns a registry populated with every provider discovered
// by ListEmbedded. Providers that fail to load are silently skipped; use
// LoadEmbedded directly if you need to surface per-provider errors.
//
// This variant does NOT enforce meta.requires.mgtt — it's for the uninstall
// and ls paths, where incompatible providers must still be visible.
func LoadAllEmbedded() *Registry {
	reg := NewRegistry()
	for _, name := range ListEmbedded() {
		if p, err := LoadEmbedded(name); err == nil {
			reg.Register(p)
		}
	}
	return reg
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

// LoadAllForUse returns a registry of every discovered provider, filtering
// out those that fail CheckCompatible. Incompatible providers are silently
// dropped — use LoadForUse on a specific name if you need the error.
func LoadAllForUse() *Registry {
	reg := NewRegistry()
	for _, name := range ListEmbedded() {
		p, err := LoadEmbedded(name)
		if err != nil {
			continue
		}
		if err := p.CheckCompatible(); err != nil {
			continue
		}
		reg.Register(p)
	}
	return reg
}
