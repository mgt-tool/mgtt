package probe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// writeFakeRunner writes a tiny shell script that emits given stdout+stderr+exit.
func writeFakeRunner(t *testing.T, stdout, stderr string, exit int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bash-based fake runner not supported on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-runner")
	script := "#!/bin/sh\n" +
		"printf '%s' " + shellQuote(stdout) + "\n" +
		"printf '%s' " + shellQuote(stderr) + " 1>&2\n" +
		"exit " + strconv.Itoa(exit) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(s string) string { return "'" + s + "'" }

func TestExternalRunner_SuccessWithStatus(t *testing.T) {
	bin := writeFakeRunner(t, `{"value":3,"raw":"3","status":"ok"}`, "", 0)
	r := NewExternalRunner(bin)
	res, err := r.Run(context.Background(), Command{Provider: "p", Component: "c", Fact: "f"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusOk {
		t.Fatalf("want ok, got %q", res.Status)
	}
	if v, ok := res.Parsed.(float64); !ok || v != 3 {
		t.Fatalf("parsed want 3, got %v (%T)", res.Parsed, res.Parsed)
	}
}

func TestExternalRunner_NotFoundStatus(t *testing.T) {
	bin := writeFakeRunner(t, `{"value":null,"raw":"","status":"not_found"}`, "", 0)
	r := NewExternalRunner(bin)
	res, err := r.Run(context.Background(), Command{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusNotFound {
		t.Fatalf("want not_found, got %q", res.Status)
	}
}

func TestExternalRunner_StatusDefaultsToOkWhenOmitted(t *testing.T) {
	bin := writeFakeRunner(t, `{"value":1,"raw":"1"}`, "", 0)
	r := NewExternalRunner(bin)
	res, err := r.Run(context.Background(), Command{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusOk {
		t.Fatalf("backward compat: omitted status should default to ok, got %q", res.Status)
	}
}

func TestExternalRunner_ClassifyForbidden(t *testing.T) {
	bin := writeFakeRunner(t, "", "rbac denied", 3)
	r := NewExternalRunner(bin)
	_, err := r.Run(context.Background(), Command{})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

func TestExternalRunner_ClassifyEnv(t *testing.T) {
	bin := writeFakeRunner(t, "", "kubectl not found", 2)
	r := NewExternalRunner(bin)
	_, err := r.Run(context.Background(), Command{})
	if !errors.Is(err, ErrEnv) {
		t.Fatalf("want ErrEnv, got %v", err)
	}
}

func TestExternalRunner_Timeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slow-runner")
	// Real provider runners fork child processes (sh → kubectl → …). Without
	// process-group killing, exec.CommandContext only kills sh; the grandchild
	// sleep keeps running and Output() hangs on its stdout pipe. This script
	// does NOT use `exec sleep`, so sh forks a sleep child — the scenario the
	// process-group kill actually needs to handle.
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewExternalRunner(path)
	start := time.Now()
	_, err := r.Run(context.Background(), Command{Timeout: 100 * time.Millisecond})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrTransient) {
		t.Fatalf("want ErrTransient, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("runner ignored timeout; elapsed %v — process group kill is not working", elapsed)
	}
}

func TestExternalRunner_ProtocolErrorOnBadJSON(t *testing.T) {
	bin := writeFakeRunner(t, "not json", "", 0)
	r := NewExternalRunner(bin)
	_, err := r.Run(context.Background(), Command{})
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("want ErrProtocol, got %v", err)
	}
}

func TestExternalRunner_RejectsUnknownStatus(t *testing.T) {
	bin := writeFakeRunner(t, `{"value":1,"raw":"1","status":"garbage"}`, "", 0)
	r := NewExternalRunner(bin)
	_, err := r.Run(context.Background(), Command{})
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("unknown status must be ErrProtocol, got %v", err)
	}
}

func TestBuildArgs_AlphabeticalAndNoNamespacePrivilege(t *testing.T) {
	args, err := buildArgs(Command{
		Component: "web", Fact: "ready", Type: "workload",
		Vars: map[string]string{"namespace": "prod", "cluster": "east"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"probe", "web", "ready", "--type", "workload",
		"--cluster", "east", "--namespace", "prod"}
	if len(args) != len(want) {
		t.Fatalf("len mismatch: got %v want %v", args, want)
	}
	for i := range args {
		if args[i] != want[i] {
			t.Fatalf("argv mismatch at %d: got %v want %v", i, args, want)
		}
	}
}

func TestBuildArgs_RejectsExtraVarsCollision(t *testing.T) {
	_, err := buildArgs(Command{
		Component: "x", Fact: "y",
		Vars:  map[string]string{"region": "us"},
		Extra: map[string]string{"region": "eu"},
	})
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("want ErrUsage on collision, got %v", err)
	}
}

func TestBuildArgs_SkipsEmptyValues(t *testing.T) {
	args, err := buildArgs(Command{
		Component: "x", Fact: "y",
		Vars: map[string]string{"a": "1", "b": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range args {
		if a == "--b" {
			t.Fatalf("empty value should be skipped: %v", args)
		}
	}
}

func TestMux_DispatchesViaInterface(t *testing.T) {
	called := ""
	stub := executorFunc(func(ctx context.Context, cmd Command) (Result, error) {
		called = cmd.Provider
		return Result{Status: StatusOk}, nil
	})
	m := &Mux{
		Default: executorFunc(func(ctx context.Context, cmd Command) (Result, error) {
			called = "default"
			return Result{Status: StatusOk}, nil
		}),
		Runners: map[string]Executor{"k8s": stub},
	}
	_, _ = m.Run(context.Background(), Command{Provider: "k8s"})
	if called != "k8s" {
		t.Fatalf("want k8s, got %q", called)
	}
	_, _ = m.Run(context.Background(), Command{Provider: "other"})
	if called != "default" {
		t.Fatalf("want default fallback, got %q", called)
	}
}

// TestImageRunner_PrependsDockerRunArgs verifies that NewImageRunner builds
// argv as ["run", "--rm", imageRef, "probe", component, fact, ...].
// We exercise buildFullArgv directly so no docker daemon is needed.
func TestImageRunner_PrependsDockerRunArgs(t *testing.T) {
	imageRef := "ghcr.io/x@sha256:abc"
	r := NewImageRunner(imageRef).(*ExternalRunner)

	if r.Binary != "docker" {
		t.Fatalf("want Binary=docker, got %q", r.Binary)
	}

	argv, err := r.buildFullArgv(Command{
		Component: "comp",
		Fact:      "running",
		Type:      "container",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"run", "--rm", imageRef, "probe", "comp", "running", "--type", "container"}
	if len(argv) != len(want) {
		t.Fatalf("argv length mismatch:\n got  %v\n want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv mismatch at [%d]: got %q want %q\nfull argv: %v", i, argv[i], want[i], argv)
		}
	}
}

// TestExternalRunner_NilArgPrefix_BackwardCompat verifies that the original
// NewExternalRunner path produces no extra prefix — only the probe protocol args.
func TestExternalRunner_NilArgPrefix_BackwardCompat(t *testing.T) {
	r := NewExternalRunner("/usr/local/bin/mgtt-provider-k8s").(*ExternalRunner)

	if len(r.ArgPrefix) != 0 {
		t.Fatalf("expected empty ArgPrefix, got %v", r.ArgPrefix)
	}

	argv, err := r.buildFullArgv(Command{
		Component: "web",
		Fact:      "ready",
		Type:      "workload",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"probe", "web", "ready", "--type", "workload"}
	if len(argv) != len(want) {
		t.Fatalf("argv length mismatch:\n got  %v\n want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv mismatch at [%d]: got %q want %q\nfull argv: %v", i, argv[i], want[i], argv)
		}
	}
}

type executorFunc func(ctx context.Context, cmd Command) (Result, error)

func (f executorFunc) Run(ctx context.Context, cmd Command) (Result, error) {
	return f(ctx, cmd)
}
