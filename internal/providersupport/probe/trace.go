// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package probe

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Tracer emits one line per probe invocation when enabled. It honors the
// MGTT_DEBUG=1 environment variable. The internal mutex serializes writes
// to W so concurrent probes don't interleave lines.
//
// Layering invariant: the trace format MUST NOT name backend-specific keys
// (namespace, region, cluster, …). It prints counts of Vars and Extra; the
// full map is only emitted under a future verbose mode that operators opt
// into explicitly.
type Tracer struct {
	Enabled bool
	W       io.Writer
	mu      sync.Mutex
}

// NewTracer reads MGTT_DEBUG and returns a Tracer that writes to stderr.
func NewTracer() *Tracer {
	return &Tracer{
		Enabled: os.Getenv("MGTT_DEBUG") == "1",
		W:       os.Stderr,
	}
}

type tracerCtxKey struct{}

// WithTracer attaches a Tracer to the context.
func WithTracer(ctx context.Context, t *Tracer) context.Context {
	if t == nil {
		return ctx
	}
	return context.WithValue(ctx, tracerCtxKey{}, t)
}

func tracerFrom(ctx context.Context) *Tracer {
	if t, ok := ctx.Value(tracerCtxKey{}).(*Tracer); ok {
		return t
	}
	return nil
}

// write serializes a single formatted line to W.
func (t *Tracer) write(format string, args ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintf(t.W, format, args...)
}

// TraceStart emits one line at probe invocation.
func TraceStart(ctx context.Context, binary string, cmd Command) {
	t := tracerFrom(ctx)
	if t == nil || !t.Enabled {
		return
	}
	t.write("[mgtt %s] probe start: %s %s.%s (type=%s vars=%d extra=%d)\n",
		time.Now().Format("15:04:05.000"), binary, cmd.Component, cmd.Fact, cmd.Type,
		len(cmd.Vars), len(cmd.Extra))
}

// TraceEnd emits one line at probe completion.
func TraceEnd(ctx context.Context, binary string, res Result, err error) {
	t := tracerFrom(ctx)
	if t == nil || !t.Enabled {
		return
	}
	if err != nil {
		t.write("[mgtt %s] probe end: %s err=%v\n",
			time.Now().Format("15:04:05.000"), binary, err)
		return
	}
	t.write("[mgtt %s] probe end: %s status=%s parsed=%v\n",
		time.Now().Format("15:04:05.000"), binary, res.Status, res.Parsed)
}
