package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// RunnerResult is the JSON structure returned by runner binaries on stdout.
type RunnerResult struct {
	Value any    `json:"value"`
	Raw   string `json:"raw"`
}

// ExternalRunner shells out to a runner binary and parses the JSON result.
type ExternalRunner struct {
	Binary string // e.g. "mgtt-provider-kubernetes"
}

// NewExternalRunner returns an ExternalRunner that calls the named binary.
func NewExternalRunner(binary string) *ExternalRunner {
	return &ExternalRunner{Binary: binary}
}

// Probe invokes the runner binary and parses its JSON output into a Result.
func (r *ExternalRunner) Probe(ctx context.Context, component, fact string, vars map[string]string) (Result, error) {
	args := []string{"probe", component, fact}
	if ns, ok := vars["namespace"]; ok {
		args = append(args, "--namespace", ns)
	}
	if typ, ok := vars["type"]; ok {
		args = append(args, "--type", typ)
	}

	cmd := exec.CommandContext(ctx, r.Binary, args...)
	out, err := cmd.Output()
	if err != nil {
		return Result{}, fmt.Errorf("runner %s: %w", r.Binary, err)
	}

	var rr RunnerResult
	if err := json.Unmarshal(out, &rr); err != nil {
		return Result{}, fmt.Errorf("runner %s: parse output: %w", r.Binary, err)
	}

	return Result{Raw: rr.Raw, Parsed: rr.Value}, nil
}
