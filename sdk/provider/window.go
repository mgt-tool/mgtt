// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Sample is one observation of a quantity.
type Sample struct {
	At    time.Time
	Value float64
}

// SeriesFn reads the samples of a quantity taken since a time: a
// CloudWatch metric's datapoints, a counter read at intervals. Any order.
type SeriesFn func(ctx context.Context, req Request, since time.Time) ([]Sample, error)

// Windowed implements a derived fact from a series. The fact spec declares
// the window and derivation (`window: 5m`, `derive: delta`); mgtt passes
// them on every probe, so one series serves every window a type declares
// over it. The result is an ordinary number.
func Windowed(series SeriesFn) ProbeFn {
	return func(ctx context.Context, req Request) (Result, error) {
		if req.Window <= 0 || req.Derive == "" {
			return Result{}, fmt.Errorf("%w: fact %q is derived, but the probe names no --window and --derive", ErrUsage, req.Fact)
		}
		samples, err := series(ctx, req, time.Now().Add(-req.Window))
		if err != nil {
			return Result{}, err
		}
		v, err := Derive(samples, req.Derive)
		if err != nil {
			return Result{}, err
		}
		res := FloatResult(v)
		res.Raw = fmt.Sprintf("%s over %s: %s", req.Derive, req.Window, res.Raw)
		return res, nil
	}
}

// Derive reduces a window's samples to one value: delta is the last sample
// minus the first, rate is that per second between them, max the largest.
// delta and rate need two samples taken at different times; a window
// holding fewer says too little yet, which is ErrTransient: the fact is
// unknown, not zero.
func Derive(samples []Sample, how string) (float64, error) {
	if len(samples) == 0 {
		return 0, fmt.Errorf("%w: no samples in the window", ErrTransient)
	}
	s := append([]Sample(nil), samples...)
	sort.SliceStable(s, func(i, j int) bool { return s[i].At.Before(s[j].At) })
	first, last := s[0], s[len(s)-1]
	switch how {
	case "max":
		m := first.Value
		for _, x := range s[1:] {
			m = max(m, x.Value)
		}
		return m, nil
	case "delta", "rate":
		span := last.At.Sub(first.At)
		if len(s) < 2 || span <= 0 {
			return 0, fmt.Errorf("%w: %s needs two samples taken apart; the window has %d", ErrTransient, how, len(s))
		}
		if how == "delta" {
			return last.Value - first.Value, nil
		}
		return (last.Value - first.Value) / span.Seconds(), nil
	default:
		return 0, fmt.Errorf("%w: derive %q: want delta, rate or max", ErrUsage, how)
	}
}
