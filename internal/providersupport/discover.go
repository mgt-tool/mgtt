// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package providersupport

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/mgt-tool/mgtt/sdk/provider"
)

// InvokeDiscover runs `<binaryPath> discover` and parses the JSON
// output into a DiscoveryResult. Non-zero exit from the provider is
// surfaced as an error — mgtt-core's callers (model-build) decide
// whether to treat that as "provider doesn't support discovery, skip"
// or to fail the build, based on context.
func InvokeDiscover(ctx context.Context, binaryPath string) (provider.DiscoveryResult, error) {
	cmd := exec.CommandContext(ctx, binaryPath, "discover")
	stdout, err := cmd.Output()
	if err != nil {
		stderrMsg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderrMsg = string(ee.Stderr)
		}
		return provider.DiscoveryResult{}, fmt.Errorf("discover %s: %w (stderr: %s)", binaryPath, err, stderrMsg)
	}
	var res provider.DiscoveryResult
	if err := json.Unmarshal(stdout, &res); err != nil {
		return provider.DiscoveryResult{}, fmt.Errorf("discover %s: parse JSON: %w (raw: %s)", binaryPath, err, truncate(stdout, 512))
	}
	return res, nil
}

// truncate returns s verbatim if len(s) <= max, otherwise the first
// max bytes with an "…(truncated)" suffix. Used to keep error
// messages from exploding when a misbehaving provider emits
// megabytes of stdout.
func truncate(s []byte, max int) string {
	if len(s) <= max {
		return string(s)
	}
	return string(s[:max]) + "…(truncated)"
}
