// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
)

// Request is the typed input handed to a ProbeFn. Type is reserved by the
// protocol (it selects the registered fact set). Everything else —
// including backend-specific keys like "region", "cluster" — lives in
// Extra, opaque to core. `namespace` is kept as a named field for SDK
// back-compat; the SDK populates both Namespace and Extra["namespace"]
// when the --namespace flag is present, so providers can read from either.
//
// The field is a PURE convenience: core does NOT default it, does NOT
// reserve the key, and does NOT short-circuit on its absence. A
// `mgtt` model that never declares a namespace variable will leave this
// empty, matching the actual state of the world.
type Request struct {
	Type      string
	Name      string
	Namespace string // shorthand for Extra["namespace"]; may be empty
	Fact      string
	Extra     map[string]string // every --<key> <value> pair from the runner argv (except --type)
}

// ProbeFn implements one fact for one type.
type ProbeFn func(ctx context.Context, req Request) (Result, error)

// Registry maps a type name to its set of fact probe functions.
type Registry struct {
	types      map[string]map[string]ProbeFn
	discoverFn func() (DiscoveryResult, error)
}

// NewRegistry creates an empty registry. Providers register each type from
// main() before calling Main(reg).
func NewRegistry() *Registry { return &Registry{types: map[string]map[string]ProbeFn{}} }

// Register adds a type's fact set. Re-registering the same type is a
// programmer bug: two different providers would silently resolve to
// whichever registered last, and a main() that calls Register twice
// for the same typ is overwriting its own setup. Panic so the author
// notices at startup rather than debugging a mysterious probe result.
func (r *Registry) Register(typ string, facts map[string]ProbeFn) {
	if _, already := r.types[typ]; already {
		panic(fmt.Sprintf("provider.Registry: type %q already registered", typ))
	}
	r.types[typ] = facts
}

// Probe dispatches to the registered ProbeFn. Errors that wrap ErrNotFound
// are translated to Result{Status: not_found} per the probe protocol.
func (r *Registry) Probe(ctx context.Context, req Request) (Result, error) {
	facts, ok := r.types[req.Type]
	if !ok {
		return Result{}, fmt.Errorf("%w: unknown type %q", ErrUsage, req.Type)
	}
	fn, ok := facts[req.Fact]
	if !ok {
		return Result{}, fmt.Errorf("%w: type %q has no fact %q", ErrUsage, req.Type, req.Fact)
	}
	res, err := fn(ctx, req)
	if errors.Is(err, ErrNotFound) {
		return NotFound(), nil
	}
	if err != nil {
		return Result{}, err
	}
	if res.Status == "" {
		res.Status = StatusOk
	}
	return res, nil
}

// RegisterDiscover installs an optional discovery function. Providers
// that want to participate in `mgtt model build` call this from main()
// before invoking provider.Main. Panics on a second call — same
// rationale as Register: double-registration is almost always a bug.
func (r *Registry) RegisterDiscover(fn func() (DiscoveryResult, error)) {
	if r.discoverFn != nil {
		panic("provider.Registry: discover function already registered")
	}
	r.discoverFn = fn
}

// Discover returns the registered discovery function and a bool
// indicating whether one was registered. Callers use the bool to
// decide whether this provider participates in model-build.
func (r *Registry) Discover() (func() (DiscoveryResult, error), bool) {
	return r.discoverFn, r.discoverFn != nil
}
