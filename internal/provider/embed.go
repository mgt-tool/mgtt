package provider

import (
	"fmt"
	"os"
	"path/filepath"
)

// LoadEmbedded loads a provider by name. It checks $MGTT_HOME/providers/<name>/
// first, then falls back to a local providers/<name>/ directory relative to CWD.
func LoadEmbedded(name string) (*Provider, error) {
	// Check $MGTT_HOME override first.
	if home := os.Getenv("MGTT_HOME"); home != "" {
		path := filepath.Join(home, "providers", name, "provider.yaml")
		if data, err := os.ReadFile(path); err == nil {
			return LoadFromBytes(data)
		}
	}

	// Fall back to local providers directory (relative to CWD).
	path := filepath.Join("providers", name, "provider.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	return LoadFromBytes(data)
}

// ListEmbedded returns the names of all providers available in $MGTT_HOME or
// the local providers/ directory.
func ListEmbedded() []string {
	var names []string

	searchDir := ""
	if home := os.Getenv("MGTT_HOME"); home != "" {
		searchDir = filepath.Join(home, "providers")
	}
	if searchDir == "" {
		searchDir = "providers"
	}

	entries, err := os.ReadDir(searchDir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() {
			yamlPath := filepath.Join(searchDir, e.Name(), "provider.yaml")
			if _, err := os.Stat(yamlPath); err == nil {
				names = append(names, e.Name())
			}
		}
	}
	return names
}
