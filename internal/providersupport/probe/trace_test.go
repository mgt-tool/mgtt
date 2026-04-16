package probe

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTracer_WritesInvocationAndEnd(t *testing.T) {
	var buf bytes.Buffer
	tr := &Tracer{Enabled: true, W: &buf}
	ctx := WithTracer(context.Background(), tr)

	TraceStart(ctx, "fakebin", Command{Component: "c", Fact: "f", Type: "workload"})
	TraceEnd(ctx, "fakebin", Result{Status: StatusOk, Parsed: 3}, nil)

	out := buf.String()
	if !strings.Contains(out, "c.f") {
		t.Fatalf("trace missing component.fact: %q", out)
	}
	if !strings.Contains(out, "type=workload") {
		t.Fatalf("trace missing type: %q", out)
	}
	if !strings.Contains(out, "status=ok") {
		t.Fatalf("trace missing status: %q", out)
	}
}

func TestTracer_LayeringInvariant_NoBackendVocabulary(t *testing.T) {
	var buf bytes.Buffer
	tr := &Tracer{Enabled: true, W: &buf}
	ctx := WithTracer(context.Background(), tr)
	TraceStart(ctx, "fakebin", Command{
		Component: "c", Fact: "f",
		Vars: map[string]string{"namespace": "prod", "cluster": "east"},
	})
	out := buf.String()
	for _, leak := range []string{"namespace", "prod", "cluster", "east"} {
		if strings.Contains(out, leak) {
			t.Fatalf("trace leaked backend vocabulary %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, "vars=2") {
		t.Fatalf("trace should include vars count: %s", out)
	}
}

func TestTracer_SilentWhenDisabled(t *testing.T) {
	var buf bytes.Buffer
	tr := &Tracer{Enabled: false, W: &buf}
	ctx := WithTracer(context.Background(), tr)
	TraceStart(ctx, "b", Command{})
	TraceEnd(ctx, "b", Result{}, nil)
	if buf.Len() != 0 {
		t.Fatalf("expected silence, got %q", buf.String())
	}
}

func TestTracer_ErrorPath(t *testing.T) {
	var buf bytes.Buffer
	tr := &Tracer{Enabled: true, W: &buf}
	ctx := WithTracer(context.Background(), tr)
	TraceEnd(ctx, "b", Result{}, errors.New("boom"))
	if !strings.Contains(buf.String(), "err=boom") {
		t.Fatalf("error trace missing message: %q", buf.String())
	}
}

func TestTracer_NoTracerInContextIsSafe(t *testing.T) {
	TraceStart(context.Background(), "b", Command{})
	TraceEnd(context.Background(), "b", Result{}, nil)
	// no panic = pass
}
