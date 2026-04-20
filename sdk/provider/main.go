package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Version is set by providers via ldflags at build time.
var Version = "dev"

// Main is the standard entrypoint. Providers call it from main().
func Main(r *Registry) {
	code := Run(context.Background(), r, os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

// Run is the testable core of Main. Returns the exit code per the probe
// protocol.
//
// Layering invariant: Run does not privilege any flag key. `--type` is the
// ONE reserved flag (it maps to Request.Type because the registry is keyed
// on type). Every other --key value pair lands in Request.Extra opaquely.
// Backend-specific keys (namespace, region, cluster, …) are not reserved
// here; providers read them from Extra as needed.
func Run(ctx context.Context, r *Registry, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: <runner> probe <name> <fact> [--type TYPE] [--<key> <value> ...]")
		return 1
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, Version)
		return 0
	case "discover":
		return runDiscover(r, stdout, stderr)
	case "probe":
		// fall through
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 1
	}
	if len(args) < 3 {
		fmt.Fprintln(stderr, "probe requires <name> and <fact>")
		return 1
	}

	req := Request{
		Name:  args[1],
		Fact:  args[2],
		Extra: map[string]string{},
	}
	for i := 3; i+1 < len(args); i += 2 {
		key, val := args[i], args[i+1]
		if !strings.HasPrefix(key, "--") {
			fmt.Fprintf(stderr, "unexpected positional arg %q (flags must use --key value)\n", key)
			return 1
		}
		k := strings.TrimPrefix(key, "--")
		if k == "type" {
			req.Type = val
			continue
		}
		req.Extra[k] = val
		// Keep the Namespace convenience field in sync with Extra. Core
		// does not privilege this key (buildArgs passes it like any other
		// flag); the field is purely SDK sugar so providers can read from
		// req.Namespace instead of req.Extra["namespace"].
		if k == "namespace" {
			req.Namespace = val
		}
	}

	res, err := r.Probe(ctx, req)
	if err != nil {
		return exitFor(err, stderr)
	}
	if err := json.NewEncoder(stdout).Encode(res); err != nil {
		fmt.Fprintln(stderr, err)
		return 5
	}
	return 0
}

// runDiscover handles the `discover` subcommand. Emits the registered
// discovery function's DiscoveryResult as JSON on stdout. Providers
// that didn't register a discovery function produce a usage error —
// callers are expected to invoke `discover` only after checking the
// provider opts in.
func runDiscover(r *Registry, stdout, stderr io.Writer) int {
	fn, ok := r.Discover()
	if !ok {
		fmt.Fprintln(stderr, "discover: this provider does not implement discovery (no RegisterDiscover call)")
		return 1
	}
	res, err := fn()
	if err != nil {
		fmt.Fprintf(stderr, "discover: %v\n", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(res); err != nil {
		fmt.Fprintln(stderr, err)
		return 5
	}
	return 0
}

func exitFor(err error, stderr io.Writer) int {
	fmt.Fprintln(stderr, err)
	switch {
	case errors.Is(err, ErrUsage):
		return 1
	case errors.Is(err, ErrEnv):
		return 2
	case errors.Is(err, ErrForbidden):
		return 3
	case errors.Is(err, ErrTransient):
		return 4
	case errors.Is(err, ErrProtocol):
		return 5
	}
	return 1
}
