package probe

import (
	"context"
	"time"
)

// Status values written by providers to indicate whether a probe is
// authoritative or whether the underlying resource was missing. See
// docs/PROBE_PROTOCOL.md.
const (
	StatusOk       = "ok"
	StatusNotFound = "not_found"
)

// Executor runs a probe command and returns the result.
type Executor interface {
	Run(ctx context.Context, cmd Command) (Result, error)
}

// Command describes a single probe to execute. The shell executor consumes
// Raw; runner-based backends consume Provider / Component / Resource / Fact /
// Type / Vars / Extra and ignore Raw.
//
// Layering invariant: core does not privilege any key in Vars or Extra.
// Both maps are passed to the runner as --<key> <value> flags in
// alphabetical order. Backend-specific names (namespace, region, cluster,
// …) live in the model and provider, never in core.
type Command struct {
	Raw       string // fully substituted command string
	Parse     string // parse mode for shell executor (int/bool/...)
	Provider  string
	Component string
	Fact      string
	Type      string            // component type, passed to runner backends as --type
	Resource  string            // upstream resource id; empty -> fall back to Component
	Vars      map[string]string // model-level variables
	Extra     map[string]string // additional flags; key collision with Vars is a usage error
	Timeout   time.Duration     // 0 = default (30s)
}

// Result holds the output of a probe execution.
type Result struct {
	Raw    string // original stdout
	Parsed any    // typed value after parsing
	Status string // StatusOk (default) or StatusNotFound
}
