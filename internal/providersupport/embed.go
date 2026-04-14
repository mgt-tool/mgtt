package providersupport

import (
	"fmt"
	"os"
	"path/filepath"
)

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
		if _, err := os.Stat(filepath.Join(candidate, "provider.yaml")); err == nil {
			return candidate
		}
	}
	return ""
}

// LoadEmbedded loads a provider by name from the search path. Uses
// LoadFromDir so multi-file providers (types/*.yaml) are supported; inline
// types in provider.yaml still work via LoadFromDir's fallback.
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
				if _, err := os.Stat(filepath.Join(dir, e.Name(), "provider.yaml")); err == nil {
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
func LoadAllEmbedded() *Registry {
	reg := NewRegistry()
	for _, name := range ListEmbedded() {
		if p, err := LoadEmbedded(name); err == nil {
			reg.Register(p)
		}
	}
	return reg
}
