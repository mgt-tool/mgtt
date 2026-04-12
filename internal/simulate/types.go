package simulate

import "github.com/sajonaro/mgtt/internal/engine"

// Scenario is a simulation scenario loaded from a YAML file.
type Scenario struct {
	Name        string                    `yaml:"name"`
	Description string                    `yaml:"description"`
	Inject      map[string]map[string]any `yaml:"inject"`
	Expect      Expectation               `yaml:"expect"`
}

// Expectation describes the expected outcome of a scenario.
type Expectation struct {
	RootCause  string   `yaml:"root_cause"`
	Path       []string `yaml:"path"`
	Eliminated []string `yaml:"eliminated"`
}

// Result holds the output of running a single scenario.
type Result struct {
	Scenario *Scenario
	Actual   Expectation
	Pass     bool
	Tree     *engine.PathTree
}
