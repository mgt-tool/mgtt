package simulate

type Scenario struct {
	Name        string                    `yaml:"name"`
	Description string                    `yaml:"description"`
	Inject      map[string]map[string]any `yaml:"inject"`
	Expect      Expectation               `yaml:"expect"`
}

type Expectation struct {
	RootCause  string   `yaml:"root_cause"`
	Path       []string `yaml:"path"`
	Eliminated []string `yaml:"eliminated"`
}

type Result struct {
	Scenario *Scenario
	Actual   Expectation
	Pass     bool
}
