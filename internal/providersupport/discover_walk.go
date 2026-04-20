package providersupport

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mgt-tool/mgtt/sdk/provider"
)

// DiscoverAll walks mgttHome/providers/ and invokes `<name>/bin/provider discover`
// for each install. Returns two maps keyed by provider name:
//   - results: successful discoveries (name → DiscoveryResult)
//   - failures: providers whose discover exited non-zero (name → error)
//
// A non-zero exit typically means "this provider doesn't support
// discovery" (older SDK version or author opted out). The caller
// decides whether that's a hard error or a warning.
func DiscoverAll(ctx context.Context, mgttHome string) (map[string]provider.DiscoveryResult, map[string]error) {
	results := map[string]provider.DiscoveryResult{}
	failures := map[string]error{}
	providersDir := filepath.Join(mgttHome, "providers")
	entries, err := os.ReadDir(providersDir)
	if err != nil {
		// Absent providers dir → nothing installed → no error, no results.
		if os.IsNotExist(err) {
			return results, failures
		}
		failures["<providers-dir>"] = err
		return results, failures
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		binary := filepath.Join(providersDir, name, "bin", "provider")
		if _, err := os.Stat(binary); err != nil {
			failures[name] = err
			continue
		}
		res, err := InvokeDiscover(ctx, binary)
		if err != nil {
			failures[name] = err
			continue
		}
		results[name] = res
	}
	return results, failures
}
