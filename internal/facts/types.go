package facts

import "time"

// StoreMeta holds metadata about a fact store (incident context).
type StoreMeta struct {
	Model    string    `yaml:"model"`
	Version  string    `yaml:"version"`
	Incident string    `yaml:"incident"`
	Started  time.Time `yaml:"started"`
	// Ended marks when the incident was closed. Zero means open.
	Ended time.Time `yaml:"ended,omitempty"`
	// Verdict is an optional human/agent note written at close.
	Verdict string `yaml:"verdict,omitempty"`
	// ModelRef is the absolute path to the model file the incident is
	// bound to. Populated by the MCP `incident.start` so downstream
	// tools (plan, probe, snapshot) can reload the model without the
	// agent having to re-pass it. Empty for CLI-started incidents.
	ModelRef string `yaml:"model_ref,omitempty"`
}

// Store is an append-only fact store. It may be in-memory or disk-backed.
// For disk-backed stores, call Save() (or AppendAndSave()) to persist.
type Store struct {
	Meta  StoreMeta
	facts map[string][]Fact
	path  string // empty for in-memory stores
}

// Fact is a single observed value for a named key on a component.
type Fact struct {
	Key       string
	Value     any
	Collector string
	At        time.Time
	Note      string
	Raw       string
	// Status carries the probe classification when Value is nil.
	// The primary consumer is "not_found": a probe ran successfully
	// but the underlying resource doesn't exist. In that case the
	// component itself should be treated as absent — scenarios
	// requiring a non-default state for that component are eliminated.
	// Empty string means "normal" (Value holds the parsed result) or
	// operator-provided facts that don't need classification.
	Status FactStatus
}

// FactStatus classifies a non-value fact. "" (default) means the fact
// carries a normal Value. Probe results with no Value set this to mark
// the reason.
type FactStatus string

const (
	// FactStatusNotFound says the probe ran successfully but the
	// component it targets doesn't exist. The live-set filter uses
	// this to eliminate any scenario that requires a non-default
	// state on the absent component.
	FactStatusNotFound FactStatus = "not_found"

	// FactStatusForbidden says the probe ran but the provider was
	// refused permission to read the underlying resource (e.g. RBAC
	// denied, IAM denied). The value is genuinely unknown — callers
	// must treat this the same as "missing fact", NOT as "false" or
	// "not_found", so an RBAC hole doesn't silently rewrite a chain.
	FactStatusForbidden FactStatus = "forbidden"

	// FactStatusTransient says the probe failed with a retryable
	// error (throttling, timeout, temporary upstream outage). Same
	// semantics as forbidden for the engine: unknown, not false.
	FactStatusTransient FactStatus = "transient"
)
