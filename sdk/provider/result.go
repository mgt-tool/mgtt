// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: Apache-2.0

package provider

import "fmt"

// Status values match the probe protocol.
const (
	StatusOk       = "ok"
	StatusNotFound = "not_found"
)

// Result is the JSON object the runner writes to stdout on success.
type Result struct {
	Value  any    `json:"value"`
	Raw    string `json:"raw"`
	Status string `json:"status,omitempty"`
}

// IntResult builds a Result for an integer value with StatusOk.
func IntResult(v int) Result { return Result{Value: v, Raw: fmt.Sprintf("%d", v), Status: StatusOk} }

// BoolResult builds a Result for a boolean value with StatusOk.
func BoolResult(v bool) Result {
	return Result{Value: v, Raw: fmt.Sprintf("%t", v), Status: StatusOk}
}

// StringResult builds a Result for a string value with StatusOk.
func StringResult(v string) Result { return Result{Value: v, Raw: v, Status: StatusOk} }

// FloatResult builds a Result for a float value with StatusOk.
func FloatResult(v float64) Result {
	return Result{Value: v, Raw: fmt.Sprintf("%g", v), Status: StatusOk}
}

// NotFound builds a Result indicating the underlying resource is missing.
// Engine translates this into an UnresolvedError with a "resource not found"
// reason. Providers SHOULD return this rather than an error when a resource
// genuinely doesn't exist — design-time simulations rely on it.
func NotFound() Result { return Result{Value: nil, Raw: "", Status: StatusNotFound} }
