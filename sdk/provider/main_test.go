package provider_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/sdk/provider"
)

func TestRun_SuccessfulProbe(t *testing.T) {
	r := provider.NewRegistry()
	r.Register("foo", map[string]provider.ProbeFn{
		"bar": func(ctx context.Context, req provider.Request) (provider.Result, error) {
			return provider.IntResult(42), nil
		},
	})
	var stdout, stderr bytes.Buffer
	code := provider.Run(context.Background(), r,
		[]string{"probe", "name", "bar", "--type", "foo"},
		&stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"value":42`) ||
		!strings.Contains(stdout.String(), `"status":"ok"`) {
		t.Fatalf("stdout missing fields: %s", stdout.String())
	}
}

func TestRun_VersionSubcommand(t *testing.T) {
	var stdout bytes.Buffer
	code := provider.Run(context.Background(), provider.NewRegistry(),
		[]string{"version"}, &stdout, &stdout)
	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatal("version should print non-empty string")
	}
}

func TestRun_NotFoundExitsZero(t *testing.T) {
	r := provider.NewRegistry()
	r.Register("foo", map[string]provider.ProbeFn{
		"bar": func(ctx context.Context, req provider.Request) (provider.Result, error) {
			return provider.Result{}, provider.ErrNotFound
		},
	})
	var stdout, stderr bytes.Buffer
	code := provider.Run(context.Background(), r,
		[]string{"probe", "missing", "bar", "--type", "foo"},
		&stdout, &stderr)
	if code != 0 {
		t.Fatalf("not_found should exit 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status":"not_found"`) {
		t.Fatalf("missing status field: %s", stdout.String())
	}
}

func TestRun_ExitCodesMatchProtocol(t *testing.T) {
	r := provider.NewRegistry()
	r.Register("foo", map[string]provider.ProbeFn{
		"forbidden": func(ctx context.Context, req provider.Request) (provider.Result, error) {
			return provider.Result{}, provider.ErrForbidden
		},
		"transient": func(ctx context.Context, req provider.Request) (provider.Result, error) {
			return provider.Result{}, provider.ErrTransient
		},
		"env": func(ctx context.Context, req provider.Request) (provider.Result, error) {
			return provider.Result{}, provider.ErrEnv
		},
		"protocol": func(ctx context.Context, req provider.Request) (provider.Result, error) {
			return provider.Result{}, provider.ErrProtocol
		},
	})
	cases := map[string]int{"forbidden": 3, "transient": 4, "env": 2, "protocol": 5}
	for fact, want := range cases {
		var stdout, stderr bytes.Buffer
		got := provider.Run(context.Background(), r,
			[]string{"probe", "x", fact, "--type", "foo"},
			&stdout, &stderr)
		if got != want {
			t.Errorf("fact %s: want exit %d, got %d stderr=%s", fact, want, got, stderr.String())
		}
	}
}

func TestRun_ExtraFlagsParsedIntoRequest(t *testing.T) {
	captured := provider.Request{}
	r := provider.NewRegistry()
	r.Register("foo", map[string]provider.ProbeFn{
		"bar": func(ctx context.Context, req provider.Request) (provider.Result, error) {
			captured = req
			return provider.IntResult(0), nil
		},
	})
	var stdout, stderr bytes.Buffer
	code := provider.Run(context.Background(), r,
		[]string{"probe", "name", "bar", "--type", "foo", "--namespace", "prod", "--region", "us-west-2"},
		&stdout, &stderr)
	if code != 0 {
		t.Fatal(stderr.String())
	}
	// Namespace is a convenience field populated alongside Extra when the
	// --namespace flag is present. Core does not default it.
	if captured.Namespace != "prod" {
		t.Fatalf("namespace not parsed: %+v", captured)
	}
	if captured.Extra["namespace"] != "prod" {
		t.Fatalf("namespace not in Extra: %+v", captured.Extra)
	}
	if captured.Extra["region"] != "us-west-2" {
		t.Fatalf("region not parsed: %+v", captured.Extra)
	}
}

func TestRun_BadSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := provider.Run(context.Background(), provider.NewRegistry(),
		[]string{"bogus"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}
