package provider

// DiscoveryResult is what a provider's `discover` subcommand emits on
// stdout. It enumerates components the provider manages and
// dependencies between them that the provider can see within its own
// domain. Cross-provider dependencies are not returned here — those
// come from catalog sources or hand-authored overrides in later
// phases.
type DiscoveryResult struct {
	Components   []DiscoveredComponent  `json:"components"`
	Dependencies []DiscoveredDependency `json:"dependencies,omitempty"`
}

// DiscoveredComponent describes one component found by provider
// discovery. Name must be stable across calls — providers normalize
// ephemeral names (e.g. pods with random suffixes) before returning.
type DiscoveredComponent struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	HealthFacts []string          `json:"health_facts,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// DiscoveredDependency is one within-provider edge.
type DiscoveredDependency struct {
	From string `json:"from"`
	To   string `json:"to"`
}
