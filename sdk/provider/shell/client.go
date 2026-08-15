// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: Apache-2.0

// Package shell is an optional SDK helper for providers that shell out to
// an external CLI (kubectl, aws, docker, ...). It handles timeouts, size
// caps, and lets each provider supply its own stderr-to-sentinel-error
// classifier.
//
// Layering note: this package is backend-agnostic. The default classifier
// EnvOnlyClassify recognizes ONE case (binary not found on PATH) and falls
// through to ErrUnknown for everything else. Providers MUST supply their
// own Classify function to get fine-grained typing of their backend's
// stderr phrasing — putting "NotFound"/"Forbidden"/"AccessDenied" string
// matching here would privilege specific backends.
package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/mgt-tool/mgtt/sdk/provider"
)

const defaultMaxBytes = 10 * 1024 * 1024
const defaultTimeout = 30 * time.Second

// ClassifyFn maps stderr + run error to a sentinel error. Providers supply
// their own to recognize backend-specific phrasing.
type ClassifyFn func(stderr string, runErr error) error

// ExecFn is the low-level command runner. Tests inject fakes here.
type ExecFn func(ctx context.Context, args ...string) (stdout, stderr []byte, err error)

// Client wraps a backend CLI binary.
type Client struct {
	Binary   string
	Timeout  time.Duration
	MaxBytes int
	Classify ClassifyFn
	Exec     ExecFn
}

// New returns a Client invoking the given binary with sensible defaults.
// Providers SHOULD set Classify to their backend-specific function.
func New(binary string) *Client {
	return &Client{
		Binary:   binary,
		Timeout:  defaultTimeout,
		MaxBytes: defaultMaxBytes,
		Classify: EnvOnlyClassify,
		Exec: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			cmd := exec.CommandContext(ctx, binary, args...)
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			return out, []byte(stderr.String()), err
		},
	}
}

// Run executes the configured binary with args. Errors are passed through
// the Classify function.
func (c *Client) Run(ctx context.Context, args ...string) ([]byte, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	stdout, stderr, err := c.Exec(ctx, args...)
	if err != nil {
		classify := c.Classify
		if classify == nil {
			classify = EnvOnlyClassify
		}
		return nil, classify(string(stderr), err)
	}
	max := c.MaxBytes
	if max == 0 {
		max = defaultMaxBytes
	}
	if len(stdout) > max {
		return nil, fmt.Errorf("%w: %s output %d bytes exceeds %d",
			provider.ErrProtocol, c.Binary, len(stdout), max)
	}
	return stdout, nil
}

// RunJSON runs the binary and unmarshals stdout as a JSON object.
func (c *Client) RunJSON(ctx context.Context, args ...string) (map[string]any, error) {
	out, err := c.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("%w: parse json: %v", provider.ErrProtocol, err)
	}
	return m, nil
}

// EnvOnlyClassify is the backend-agnostic default. It handles ONE case:
// the backend CLI binary is not on PATH (exec.ErrNotFound → ErrEnv).
// Everything else falls through to ErrUnknown — providers should supply
// their own Classify for fine-grained typing of their backend's phrasing.
func EnvOnlyClassify(stderr string, runErr error) error {
	if errors.Is(runErr, exec.ErrNotFound) {
		return fmt.Errorf("%w: %v", provider.ErrEnv, runErr)
	}
	if runErr != nil {
		return fmt.Errorf("%w: %s", provider.ErrUnknown, firstLine(stderr))
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
