package registry

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultRegistryURL = "https://mgt-tool.github.io/mgtt/registry.yaml"
const cacheTTL = 24 * time.Hour

// Entry is a single provider in the registry. Tags summarise the
// provider's coverage at a high level — the authoritative type list lives in
// the provider's own provider.yaml.
type Entry struct {
	URL         string   `yaml:"url"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
}

// Registry is the parsed registry index.
type Registry struct {
	Providers map[string]Entry `yaml:"providers"`
}

// Lookup returns the entry for a provider name.
func (r *Registry) Lookup(name string) (Entry, bool) {
	e, ok := r.Providers[name]
	return e, ok
}

// Fetch retrieves the registry, using a local cache if fresh.
// Set noCache to bypass the cache.
func Fetch(noCache bool) (*Registry, error) {
	url := DefaultRegistryURL
	if env := os.Getenv("MGTT_REGISTRY_URL"); env != "" {
		url = env
	}

	cachePath := cacheFilePath()

	// Try cache first.
	if !noCache && cachePath != "" {
		if info, err := os.Stat(cachePath); err == nil {
			if time.Since(info.ModTime()) < cacheTTL {
				data, err := os.ReadFile(cachePath)
				if err == nil {
					var reg Registry
					if err := yaml.Unmarshal(data, &reg); err == nil {
						return &reg, nil
					}
				}
			}
		}
	}

	// Fetch from network.
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch registry: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB limit
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}

	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}

	// Write cache.
	if cachePath != "" {
		os.MkdirAll(filepath.Dir(cachePath), 0755)
		os.WriteFile(cachePath, data, 0644)
	}

	return &reg, nil
}

func cacheFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".mgtt", "cache", "registry.yaml")
}
