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
		return provider.DiscoveryResult{}, fmt.Errorf("discover %s: parse JSON: %w (raw: %s)", binaryPath, err, string(stdout))
	}
	return res, nil
}
