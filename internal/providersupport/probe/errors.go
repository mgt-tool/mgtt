// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package probe

import (
	"fmt"
	"strings"

	"github.com/mgt-tool/mgtt/sdk/provider"
)

// Sentinel errors form the typed error taxonomy returned by ExternalRunner.
// Exit codes from the provider runner map to these per the probe protocol
// (see docs/PROBE_PROTOCOL.md). Aliased to the SDK-owned sentinels so
// errors.Is crosses the provider/core boundary — a provider that
// `return fmt.Errorf("%w: ...", provider.ErrUsage)` is indistinguishable
// from one where core classifies exit code 1 → probe.ErrUsage.
var (
	ErrUsage     = provider.ErrUsage
	ErrEnv       = provider.ErrEnv
	ErrForbidden = provider.ErrForbidden
	ErrTransient = provider.ErrTransient
	ErrProtocol  = provider.ErrProtocol
	ErrUnknown   = provider.ErrUnknown
)

// ClassifyExit maps a runner exit code + stderr line to a sentinel error.
// See docs/PROBE_PROTOCOL.md for the canonical mapping.
func ClassifyExit(code int, stderr string) error {
	msg := FirstLine(stderr)
	switch code {
	case 1:
		return fmt.Errorf("%w: %s", ErrUsage, msg)
	case 2:
		return fmt.Errorf("%w: %s", ErrEnv, msg)
	case 3:
		return fmt.Errorf("%w: %s", ErrForbidden, msg)
	case 4:
		return fmt.Errorf("%w: %s", ErrTransient, msg)
	case 5:
		return fmt.Errorf("%w: %s", ErrProtocol, msg)
	}
	return fmt.Errorf("%w: exit %d: %s", ErrUnknown, code, msg)
}

// FirstLine returns the first line of s, trimmed of surrounding whitespace.
// Shared across the probe and CLI layers for one-line error rendering.
func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
