package engine

import "github.com/mgt-tool/mgtt/internal/state"

// PathTree is the output of the Plan function — the complete analysis of
// failure paths from an entry point through the dependency graph.
type PathTree struct {
	Entry      string
	Paths      []Path
	Eliminated []Path
	Suggested  *Probe
	RootCause  string
	States     *state.Derivation
}

// Path represents a single failure path through the dependency graph.
type Path struct {
	ID         string
	Components []string
	Reason     string // why eliminated, if eliminated
}

// Probe is a suggested next step — which fact to collect to narrow down the
// root cause.
type Probe struct {
	Component  string
	Fact       string
	Provider   string   // owning provider name
	ParseMode  string   // from factSpec.Probe.Parse
	Eliminates []string // path IDs
	Cost       string
	Access     string
	Command    string   // raw template (pre-substitution)
}
