// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

type Scenario struct {
	Name        string                    `yaml:"name"`
	Description string                    `yaml:"description"`
	Inject      map[string]map[string]any `yaml:"inject"`
	Expect      Expectation               `yaml:"expect"`
	// UnenumeratedIntentional suppresses the gap-detection warning when
	// the case's expected root has no matching enumerated scenario.
	// Set this when the case deliberately exercises a hypothetical
	// failure mode that the model's triggered_by graph doesn't yet
	// cover.
	UnenumeratedIntentional bool `yaml:"unenumerated_intentional"`
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
