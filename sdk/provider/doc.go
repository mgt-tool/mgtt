// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: Apache-2.0

// Package provider is the SDK for building mgtt provider runner binaries.
//
// A minimal provider:
//
//	package main
//
//	import (
//	    "context"
//	    "github.com/mgt-tool/mgtt/sdk/provider"
//	)
//
//	func main() {
//	    r := provider.NewRegistry()
//	    r.Register("deployment", map[string]provider.ProbeFn{
//	        "ready_replicas": func(ctx context.Context, req provider.Request) (provider.Result, error) {
//	            // ... shell out, parse, return
//	            return provider.IntResult(3), nil
//	        },
//	    })
//	    provider.Main(r)
//	}
//
// See docs/PROBE_PROTOCOL.md in the mgtt repo for the wire contract.
//
// Layering note: this SDK is backend-agnostic. It does not import any
// kubernetes/aws/docker code, and it does not match backend-specific stderr
// strings. The shell sub-package provides a generic CLI invocation helper
// that providers can wrap with their own Classify function.
package provider
