// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: Apache-2.0

package shell

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/sdk/provider"
)

func TestClient_Run_PassThroughStdout(t *testing.T) {
	c := &Client{Exec: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		return []byte("hello"), nil, nil
	}}
	out, err := c.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello" {
		t.Fatalf("got %q", out)
	}
}

func TestClient_Run_SizeCap(t *testing.T) {
	big := make([]byte, 11*1024*1024)
	c := &Client{
		MaxBytes: 10 * 1024 * 1024,
		Exec: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			return big, nil, nil
		},
	}
	_, err := c.Run(context.Background(), "x")
	if !errors.Is(err, provider.ErrProtocol) {
		t.Fatalf("want ErrProtocol, got %v", err)
	}
}

func TestClient_Run_Timeout(t *testing.T) {
	c := &Client{
		Timeout: 1 * time.Millisecond,
		Exec: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			<-ctx.Done()
			return nil, []byte("deadline exceeded"), ctx.Err()
		},
		Classify: EnvOnlyClassify,
	}
	_, err := c.Run(context.Background(), "x")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestEnvOnlyClassify_BinaryNotFound(t *testing.T) {
	err := EnvOnlyClassify("", &exec.Error{Name: "kubectl", Err: exec.ErrNotFound})
	if !errors.Is(err, provider.ErrEnv) {
		t.Fatalf("want ErrEnv, got %v", err)
	}
}

func TestEnvOnlyClassify_FallsThroughToUnknown(t *testing.T) {
	err := EnvOnlyClassify("some weird error from kubectl", errors.New("exit status 1"))
	if !errors.Is(err, provider.ErrUnknown) {
		t.Fatalf("default should not classify backend-specific errors; want ErrUnknown, got %v", err)
	}
}

func TestClient_RunJSON(t *testing.T) {
	c := &Client{Exec: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		return []byte(`{"a":1,"b":"x"}`), nil, nil
	}}
	got, err := c.RunJSON(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != float64(1) || got["b"] != "x" {
		t.Fatalf("parse: %+v", got)
	}
}

func TestClient_RunJSON_BadJSONErrProtocol(t *testing.T) {
	c := &Client{Exec: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		return []byte("not json"), nil, nil
	}}
	_, err := c.RunJSON(context.Background(), "x")
	if !errors.Is(err, provider.ErrProtocol) {
		t.Fatalf("want ErrProtocol, got %v", err)
	}
}

func TestClient_CustomClassify(t *testing.T) {
	// Provider-supplied classifier: maps "NotFound" stderr to ErrNotFound.
	classify := func(stderr string, runErr error) error {
		if runErr == nil {
			return nil
		}
		if strings.Contains(stderr, "NotFound") {
			return provider.ErrNotFound
		}
		return provider.ErrUnknown
	}
	c := &Client{
		Classify: classify,
		Exec: func(ctx context.Context, args ...string) ([]byte, []byte, error) {
			return nil, []byte("Error from server (NotFound): deployments.apps \"x\" not found"),
				errors.New("exit status 1")
		},
	}
	_, err := c.Run(context.Background(), "x")
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("custom classify failed: %v", err)
	}
}
