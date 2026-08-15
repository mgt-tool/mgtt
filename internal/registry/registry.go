// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package registry resolves provider names to git URLs via a YAML index.
// The index location is configurable per-invocation; corporate operators
// running behind air-gaps or on internal mirrors override the default.
//
// Resolution sources, in priority order at every Fetch call:
//
//  1. Explicit Source argument (e.g. from a --registry CLI flag).
//  2. MGTT_REGISTRY_URL environment variable.
//  3. The default community registry (DefaultRegistryURL).
//
// Special MGTT_REGISTRY_URL values:
//
//	"disabled" / "none" / "off"  → no registry resolution; Fetch returns
//	                                ErrRegistryDisabled. Callers (typically
//	                                `mgtt provider install`) should fall
//	                                through to git-URL / local-path inputs.
//
//	"file:///path/to/registry.yaml" → load from a local file. Useful for
//	                                  air-gapped installs that ship the
//	                                  index alongside.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultRegistryURL = "https://mgt-tool.github.io/mgtt/registry.yaml"
const cacheTTL = 24 * time.Hour

// ErrRegistryDisabled is returned when the operator has opted out of
// registry resolution (MGTT_REGISTRY_URL=disabled, "none", or "off").
// Callers should treat this as "fall through to other resolution paths"
// rather than as a network-style failure.
var ErrRegistryDisabled = errors.New("registry: disabled by configuration")

// Entry is a single provider in the registry. Mirrors the provider's
// `needs:` and `network:` declarations so registry readers see the
// runtime posture (host resources forwarded, docker network mode) at
// a glance before they commit to pulling the provider.
type Entry struct {
	URL          string   `yaml:"url"`
	Image        string   `yaml:"image,omitempty"`
	Description  string   `yaml:"description"`
	Tags         []string `yaml:"tags"`
	Capabilities []string `yaml:"capabilities,omitempty"`
	Network      string   `yaml:"network,omitempty"` // bridge (default, omitted), host, none
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

// Source describes where the registry index comes from for a single Fetch
// call. Zero-value means "use env var, then default."
type Source struct {
	// URL overrides MGTT_REGISTRY_URL when non-empty (e.g. from a
	// --registry CLI flag). Same sentinel values apply.
	URL string

	// NoCache bypasses the on-disk cache for this fetch.
	NoCache bool
}

// Fetch retrieves the registry. See package doc for resolution priority.
func Fetch(src Source) (*Registry, error) {
	rawURL := resolveURL(src.URL)
	if isDisabled(rawURL) {
		return nil, ErrRegistryDisabled
	}
	if strings.HasPrefix(rawURL, "file://") {
		path, err := fileURLPath(rawURL)
		if err != nil {
			return nil, err
		}
		return loadFromFile(path)
	}
	// Cache is keyed on URL so mirror-switching at --registry time
	// never serves content fetched against a different identity.
	cachePath := cacheFilePath(rawURL)
	if !src.NoCache {
		if reg := readCache(cachePath); reg != nil {
			return reg, nil
		}
	}
	data, err := fetchRegistryBytes(rawURL)
	if err != nil {
		return nil, err
	}
	reg, err := parseRegistryBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	writeCache(cachePath, data)
	return reg, nil
}

// readCache returns a parsed Registry from cachePath when one exists,
// is fresh (within cacheTTL), and parses cleanly. Any failure at any
// step falls through to nil so the caller re-fetches.
func readCache(cachePath string) *Registry {
	if cachePath == "" {
		return nil
	}
	info, err := os.Stat(cachePath)
	if err != nil || time.Since(info.ModTime()) >= cacheTTL {
		return nil
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil
	}
	reg, err := parseRegistryBytes(data)
	if err != nil {
		return nil
	}
	return reg
}

// fetchRegistryBytes fetches rawURL with a 15s timeout and a 1 MiB cap
// on the response body. Non-200s surface as structured errors.
func fetchRegistryBytes(rawURL string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch registry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch registry: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	return data, nil
}

// writeCache persists data to cachePath, creating parent directories.
// Cache writes are best-effort — failures don't surface to the caller.
func writeCache(cachePath string, data []byte) {
	if cachePath == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
	_ = os.WriteFile(cachePath, data, 0o644)
}

// parseRegistryBytes unmarshals a registry index from raw YAML. Shared
// across the network / cache / file-URL load paths.
func parseRegistryBytes(data []byte) (*Registry, error) {
	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	return &reg, nil
}

// fileURLPath parses a file:// URL per RFC 8089 and returns the local path.
// Accepts file:///abs/path and file://localhost/abs/path; rejects other
// authorities (file://other-host/path) because mgtt cannot reach them.
// Performs percent-decoding so file:///opt/my%20registry.yaml works.
func fileURLPath(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse file URL %q: %w", raw, err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("not a file URL: %q", raw)
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("file URL host %q not supported (use file:///path or file://localhost/path)", u.Host)
	}
	// u.Path is already percent-decoded by net/url.
	return filepath.FromSlash(u.Path), nil
}

// resolveURL applies the priority chain: explicit override > env var > default.
func resolveURL(override string) string {
	if override != "" {
		return override
	}
	if env := os.Getenv("MGTT_REGISTRY_URL"); env != "" {
		return env
	}
	return DefaultRegistryURL
}

// isDisabled recognises the documented sentinel values.
func isDisabled(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "disabled", "none", "off":
		return true
	}
	return false
}

// loadFromFile reads + parses a local registry index. Used for air-gapped
// installs (file:// URLs).
func loadFromFile(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registry file %q: %w", path, err)
	}
	reg, err := parseRegistryBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse registry file %q: %w", path, err)
	}
	return reg, nil
}

// cacheFilePath returns a per-URL cache path so distinct registry sources
// don't cross-contaminate. Honors MGTT_HOME for the cache root when set.
func cacheFilePath(registryURL string) string {
	root := ""
	if home := os.Getenv("MGTT_HOME"); home != "" {
		root = home
	} else if h, err := os.UserHomeDir(); err == nil {
		root = filepath.Join(h, ".mgtt")
	}
	if root == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(registryURL))
	name := hex.EncodeToString(sum[:8]) + ".yaml"
	return filepath.Join(root, "cache", "registry", name)
}
