// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"testing"
)

func TestRegistry_UnknownType(t *testing.T) {
	r := NewRegistry()
	_, err := r.Probe(context.Background(), Request{Type: "bogus", Fact: "x"})
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("expected ErrUsage, got %v", err)
	}
}

func TestRegistry_UnknownFact(t *testing.T) {
	r := NewRegistry()
	r.Register("foo", map[string]ProbeFn{
		"known": func(ctx context.Context, req Request) (Result, error) { return IntResult(1), nil },
	})
	_, err := r.Probe(context.Background(), Request{Type: "foo", Fact: "missing"})
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("expected ErrUsage, got %v", err)
	}
}

func TestRegistry_DispatchAndStatusDefault(t *testing.T) {
	r := NewRegistry()
	r.Register("foo", map[string]ProbeFn{
		"bar": func(ctx context.Context, req Request) (Result, error) {
			return Result{Value: 42, Raw: "42"}, nil
		},
	})
	got, err := r.Probe(context.Background(), Request{Type: "foo", Fact: "bar"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != 42 {
		t.Fatalf("expected 42, got %v", got.Value)
	}
	if got.Status != StatusOk {
		t.Fatalf("Status should default to ok, got %q", got.Status)
	}
}

func TestRegistry_NotFoundErrTranslatesToStatus(t *testing.T) {
	r := NewRegistry()
	r.Register("foo", map[string]ProbeFn{
		"bar": func(ctx context.Context, req Request) (Result, error) {
			return Result{}, ErrNotFound
		},
	})
	got, err := r.Probe(context.Background(), Request{Type: "foo", Fact: "bar"})
	if err != nil {
		t.Fatalf("not_found should not surface as error: %v", err)
	}
	if got.Status != StatusNotFound {
		t.Fatalf("want not_found, got %q", got.Status)
	}
	if got.Value != nil {
		t.Fatalf("Value should be nil on not_found, got %v", got.Value)
	}
}

// TestRegistry_DoubleRegisterPanics locks in the contract documented
// on Register: a second call for the same type is a programmer bug and
// panics. Regression guard for sdk callers that accidentally
// re-register on config reload.
func TestRegistry_DoubleRegisterPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on double-Register; got none")
		}
	}()
	r := NewRegistry()
	fn := func(ctx context.Context, req Request) (Result, error) { return IntResult(1), nil }
	r.Register("foo", map[string]ProbeFn{"x": fn})
	r.Register("foo", map[string]ProbeFn{"x": fn}) // must panic
}

// TestRegistry_DoubleRegisterDiscoverPanics mirrors the above for
// RegisterDiscover — second install of a discovery function is a bug.
func TestRegistry_DoubleRegisterDiscoverPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on double-RegisterDiscover; got none")
		}
	}()
	r := NewRegistry()
	disc := func() (DiscoveryResult, error) { return DiscoveryResult{}, nil }
	r.RegisterDiscover(disc)
	r.RegisterDiscover(disc) // must panic
}
