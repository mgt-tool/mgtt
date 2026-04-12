package simulate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// LoadScenario reads and parses a single scenario YAML file.
func LoadScenario(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenario %q: %w", path, err)
	}

	var sc Scenario
	if err := yaml.Unmarshal(data, &sc); err != nil {
		return nil, fmt.Errorf("parse scenario %q: %w", path, err)
	}

	// Normalise injected values: YAML decodes integers as int, but some
	// environments produce float64. Coerce float64 values that are whole
	// numbers to int so the fact store and expression evaluator see int.
	for comp := range sc.Inject {
		for k, v := range sc.Inject[comp] {
			sc.Inject[comp][k] = normaliseValue(v)
		}
	}

	return &sc, nil
}

// LoadAllScenarios loads every *.yaml file in dir, sorted by filename.
func LoadAllScenarios(dir string) ([]*Scenario, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("glob scenarios in %q: %w", dir, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no scenario files found in %q", dir)
	}

	sort.Strings(matches)

	scenarios := make([]*Scenario, 0, len(matches))
	for _, path := range matches {
		sc, err := LoadScenario(path)
		if err != nil {
			return nil, err
		}
		scenarios = append(scenarios, sc)
	}
	return scenarios, nil
}

// normaliseValue coerces YAML-decoded values to the types expected by the
// fact store and expression engine:
//   - float64 that is a whole number → int
//   - everything else unchanged
func normaliseValue(v any) any {
	switch x := v.(type) {
	case float64:
		if x == float64(int(x)) {
			return int(x)
		}
		return x
	case int:
		return x
	default:
		return v
	}
}
