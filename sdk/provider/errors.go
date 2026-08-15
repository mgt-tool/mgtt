// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: Apache-2.0

package provider

import "errors"

// Sentinel errors mirror the core probe package's error taxonomy. Providers
// return them; sdk.Main maps them to runner exit codes per the probe protocol.
//
// These are duplicated (not re-exported from internal/) so external provider
// modules can import them without depending on mgtt internals.
var (
	ErrUsage     = errors.New("provider: usage error")
	ErrEnv       = errors.New("provider: environment error")
	ErrForbidden = errors.New("provider: forbidden")
	ErrTransient = errors.New("provider: transient error")
	ErrProtocol  = errors.New("provider: protocol error")
	ErrUnknown   = errors.New("provider: unknown error")

	// ErrNotFound: a probe function returns this when the underlying resource
	// does not exist. The SDK converts it to Result{Status: not_found} —
	// providers do NOT need to call NotFound() explicitly when they can
	// distinguish the missing case via a typed error from their backend.
	ErrNotFound = errors.New("provider: not found")
)
