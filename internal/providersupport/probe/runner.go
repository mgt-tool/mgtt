package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	defaultTimeout = 30 * time.Second
	maxStdoutBytes = 10 * 1024 * 1024
)

// ExternalRunner invokes a provider's runner binary and parses its JSON
// result. It implements Executor so it can be composed in a Mux.
//
// When ArgPrefix is non-empty, every exec.Command is built as:
//
//	Binary ArgPrefix[0] ArgPrefix[1] … <probe-args>
//
// This is used by NewImageRunner to wrap the provider binary in
// "docker run --rm <imageRef>".
type ExternalRunner struct {
	Binary    string
	ArgPrefix []string
}

// NewExternalRunner returns an Executor that shells out to the given
// provider runner binary. The return type is the Executor interface (not
// the concrete struct) so callers cannot depend on internal layout.
func NewExternalRunner(binary string) Executor {
	return &ExternalRunner{Binary: binary}
}

// NewImageRunner returns an Executor that runs the provider via
//
//	docker run --rm <cap-flags…> <imageRef> <probe-args…>
//
// The image's ENTRYPOINT must be the provider binary; probe args are
// passed through unchanged per the standard probe protocol. `needs` is
// the provider's declared image.needs list; each label is expanded into
// bind-mount/env-forward flags by capabilities.Apply. A nil or empty
// needs yields the legacy no-forwarding shape.
func NewImageRunner(imageRef string, needs []string) Executor {
	args := []string{"run", "--rm"}
	args = append(args, Apply(needs)...)
	args = append(args, imageRef)
	return &ExternalRunner{
		Binary:    "docker",
		ArgPrefix: args,
	}
}

// buildFullArgv returns the complete argument list passed to exec.Command:
// ArgPrefix (if any) followed by the probe protocol args derived from cmd.
// It is the single authoritative place that constructs the full argv and is
// extracted so tests can verify the shape without forking a process.
func (r *ExternalRunner) buildFullArgv(cmd Command) ([]string, error) {
	args, err := buildArgs(cmd)
	if err != nil {
		return nil, err
	}
	return append(append([]string(nil), r.ArgPrefix...), args...), nil
}

// Run implements Executor.
func (r *ExternalRunner) Run(ctx context.Context, cmd Command) (res Result, err error) {
	TraceStart(ctx, r.Binary, cmd)
	defer func() { TraceEnd(ctx, r.Binary, res, err) }()

	argv, err := r.buildFullArgv(cmd)
	if err != nil {
		return Result{}, err
	}

	timeout := cmd.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c := exec.CommandContext(ctx, r.Binary, argv...)
	// Put the child in its own process group so we can kill the entire
	// subtree on timeout. exec.CommandContext only kills the direct child;
	// real provider runners fork kubectl/aws/etc., which would orphan.
	setProcessGroup(c)
	c.Cancel = func() error { return killProcessGroup(c) }

	var stderr strings.Builder
	c.Stderr = &stderr
	stdout, runErr := c.Output()

	if runErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Result{}, fmt.Errorf("%w: runner %s exceeded %s", ErrTransient, r.Binary, timeout)
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return Result{}, ClassifyExit(exitErr.ExitCode(), stderr.String())
		}
		return Result{}, fmt.Errorf("%w: runner %s: %w", ErrEnv, r.Binary, runErr)
	}

	if len(stdout) > maxStdoutBytes {
		return Result{}, fmt.Errorf("%w: runner %s output %d bytes exceeds cap %d",
			ErrProtocol, r.Binary, len(stdout), maxStdoutBytes)
	}

	var rr struct {
		Value  any    `json:"value"`
		Raw    string `json:"raw"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout, &rr); err != nil {
		return Result{}, fmt.Errorf("%w: parse runner %s output: %w", ErrProtocol, r.Binary, err)
	}
	// Whitelist status values. Omitted defaults to ok (back-compat with
	// pre-1.1 providers). Anything else is a protocol violation.
	switch rr.Status {
	case "":
		rr.Status = StatusOk
	case StatusOk, StatusNotFound:
		// accepted
	default:
		return Result{}, fmt.Errorf("%w: runner %s returned unknown status %q",
			ErrProtocol, r.Binary, rr.Status)
	}
	return Result{Raw: rr.Raw, Parsed: rr.Value, Status: rr.Status}, nil
}

// setProcessGroup places the child in a new process group so killing the
// group kills all descendants. Unix-only; Windows would need JobObject.
func setProcessGroup(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setpgid = true
}

// killProcessGroup sends SIGKILL to the child's entire process group.
func killProcessGroup(c *exec.Cmd) error {
	if c.Process == nil {
		return nil
	}
	// Negative PID = process group; we set Setpgid above so pgid == pid.
	return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
}

// buildArgs constructs the runner argv per the probe protocol:
//
//	probe <component> <fact> [--type T] [--<key> <value> ...]
//
// All Vars and Extra entries are passed as flags in alphabetical key order.
// Key collisions between Vars and Extra are a usage error — caller must
// resolve them before invoking Run.
func buildArgs(cmd Command) ([]string, error) {
	for k := range cmd.Extra {
		if _, conflict := cmd.Vars[k]; conflict {
			return nil, fmt.Errorf("%w: key %q present in both Vars and Extra", ErrUsage, k)
		}
	}
	merged := make(map[string]string, len(cmd.Vars)+len(cmd.Extra))
	for k, v := range cmd.Vars {
		if v != "" {
			merged[k] = v
		}
	}
	for k, v := range cmd.Extra {
		if v != "" {
			merged[k] = v
		}
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	args := []string{"probe", cmd.Component, cmd.Fact}
	if cmd.Type != "" {
		args = append(args, "--type", cmd.Type)
	}
	for _, k := range keys {
		args = append(args, "--"+k, merged[k])
	}
	return args, nil
}

// Mux dispatches commands to a per-provider Executor, falling back to Default.
// Runners is keyed by provider name and typed as the Executor interface so
// any implementation (test fakes, alternate backends) plugs in uniformly.
type Mux struct {
	Default Executor
	Runners map[string]Executor
}

func (m *Mux) Run(ctx context.Context, cmd Command) (Result, error) {
	if r, ok := m.Runners[cmd.Provider]; ok {
		return r.Run(ctx, cmd)
	}
	return m.Default.Run(ctx, cmd)
}
