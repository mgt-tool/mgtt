// Package exec provides a probe.Executor that runs real shell commands via
// os/exec. This is the production backend; tests should use the fixture backend.
package exec

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"mgtt/internal/probe"
)

// Executor runs probe commands by invoking them through the system shell.
type Executor struct{}

// Default returns a new Executor with default settings.
func Default() *Executor { return &Executor{} }

// Run executes cmd.Raw via "sh -c", applies the timeout (default 30s), parses
// stdout with cmd.Parse, and returns a Result.
func (e *Executor) Run(ctx context.Context, cmd probe.Command) (probe.Result, error) {
	timeout := cmd.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c := exec.CommandContext(ctx, "sh", "-c", cmd.Raw)
	out, err := c.Output()

	exitCode := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return probe.Result{}, fmt.Errorf("exec failed: %w", err)
		}
		exitCode = exitErr.ExitCode()
	}

	parsed, parseErr := probe.ParseOutput(cmd.Parse, string(out), exitCode)
	return probe.Result{
		Raw:      string(out),
		Parsed:   parsed,
		ExitCode: exitCode,
	}, parseErr
}
