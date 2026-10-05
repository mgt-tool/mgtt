// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestDerive(t *testing.T) {
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	at := func(sec int, v float64) Sample { return Sample{At: t0.Add(time.Duration(sec) * time.Second), Value: v} }
	climbing := []Sample{at(300, 4200), at(0, 40), at(120, 900)} // out of order on purpose
	for _, c := range []struct {
		how  string
		want float64
	}{{"delta", 4160}, {"rate", 4160.0 / 300}, {"max", 4200}} {
		got, err := Derive(climbing, c.how)
		if err != nil || got != c.want {
			t.Errorf("%s: got %v, %v; want %v", c.how, got, err, c.want)
		}
	}
	if _, err := Derive([]Sample{at(0, 1)}, "delta"); !errors.Is(err, ErrTransient) {
		t.Errorf("one sample: delta should be transient, got %v", err)
	}
	if v, err := Derive([]Sample{at(0, 7)}, "max"); err != nil || v != 7 {
		t.Errorf("one sample: max is the sample; got %v, %v", v, err)
	}
	if _, err := Derive(nil, "max"); !errors.Is(err, ErrTransient) {
		t.Errorf("no samples should be transient, got %v", err)
	}
	if _, err := Derive(climbing, "median"); !errors.Is(err, ErrUsage) {
		t.Errorf("an unknown derivation is a usage error, got %v", err)
	}
}

func TestWindowed(t *testing.T) {
	var since time.Time
	fn := Windowed(func(_ context.Context, _ Request, s time.Time) ([]Sample, error) {
		since = s
		now := time.Now()
		return []Sample{{At: now.Add(-4 * time.Minute), Value: 3}, {At: now, Value: 11}}, nil
	})
	res, err := fn(context.Background(), Request{Fact: "restart_count_delta_15m", Window: 15 * time.Minute, Derive: "delta"})
	if err != nil || res.Value != 8.0 {
		t.Fatalf("got %+v, %v; want 8", res, err)
	}
	if ago := time.Since(since); ago < 15*time.Minute || ago > 16*time.Minute {
		t.Errorf("the series was read since %s ago; want the 15m window", ago)
	}
	if _, err := fn(context.Background(), Request{Fact: "restart_count_delta_15m"}); !errors.Is(err, ErrUsage) {
		t.Errorf("a derived fact probed without --window and --derive is a usage error, got %v", err)
	}
}

// --window and --derive are reserved: they reach the request's own fields,
// not Extra, and a window that is not a duration is a usage error.
func TestRun_DerivedFactFlags(t *testing.T) {
	var got Request
	r := NewRegistry()
	r.Register("broker", map[string]ProbeFn{"queue_depth_delta_5m": func(_ context.Context, req Request) (Result, error) {
		got = req
		return IntResult(1), nil
	}})
	var out, errOut bytes.Buffer
	code := Run(context.Background(), r, []string{"probe", "orders", "queue_depth_delta_5m", "--type", "broker", "--window", "5m", "--derive", "delta", "--region", "eu-central-1"}, &out, &errOut)
	if code != 0 || got.Window != 5*time.Minute || got.Derive != "delta" || got.Extra["region"] != "eu-central-1" || len(got.Extra) != 1 {
		t.Fatalf("exit %d, request %+v, stderr %q", code, got, errOut.String())
	}
	if code := Run(context.Background(), r, []string{"probe", "orders", "queue_depth_delta_5m", "--type", "broker", "--window", "soon"}, &out, &errOut); code != 1 {
		t.Errorf("a window that is not a duration: exit %d, want 1", code)
	}
}
