// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package providersupport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mgt-tool/mgtt/sdk/provider"
)

// DiscoverAll walks mgttHome/providers/ and invokes `<name>/bin/provider discover`
// for each install. Returns:
//   - results: successful discoveries (name → DiscoveryResult)
//   - failures: providers whose discover exited non-zero (name → error);
//     errors.Is(err, ErrNoDiscover) when the provider has no discovery
//   - homeErr: error reading the providers directory itself (nil when the
//     directory simply doesn't exist — that is a legitimate empty state)
//
// A failure is either "this provider doesn't support discovery" (no
// binary, older SDK version, or author opted out — ErrNoDiscover) or a
// discovery that ran and failed (timeout, backend error, bad JSON). The
// caller decides whether either is a hard error or a warning.
func DiscoverAll(ctx context.Context, mgttHome string) (results map[string]provider.DiscoveryResult, failures map[string]error, homeErr error) {
	results = map[string]provider.DiscoveryResult{}
	failures = map[string]error{}
	providersDir := filepath.Join(mgttHome, "providers")
	entries, err := os.ReadDir(providersDir)
	if err != nil {
		// Absent providers dir → nothing installed → no error, no results.
		if os.IsNotExist(err) {
			return results, failures, nil
		}
		return results, failures, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		binary := filepath.Join(providersDir, name, "bin", "provider")
		if _, err := os.Stat(binary); err != nil {
			if os.IsNotExist(err) {
				// Types-only provider: no binary, so nothing to discover.
				err = fmt.Errorf("%w (no %s)", ErrNoDiscover, binary)
			}
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
	return results, failures, nil
}
