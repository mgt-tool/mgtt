// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

type Scenario struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// Entry is where diagnosis starts: the component the failure is seen
	// at. Empty means the model's entry point, the first component nothing
	// depends on; a failure only another top-level component shows, such as
	// a background job, needs that component named here.
	Entry  string                    `yaml:"entry"`
	Inject map[string]map[string]any `yaml:"inject"`
	// Unresolved records probes that ran without producing a value:
	// component → fact → not_found | forbidden | transient. It is how a
	// scenario says "the probe was refused" or "the resource is gone",
	// which a value in inject cannot.
	Unresolved map[string]map[string]string `yaml:"unresolved"`
	Expect     Expectation                  `yaml:"expect"`
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
	// NotEliminated lists components that must stay in play: the claim
	// a scenario about a refused or failed probe exists to make.
	// Eliminated is a subset check and cannot say it.
	NotEliminated []string `yaml:"not_eliminated"`
	// CannotRuleOut lists components the conclusion must report as
	// undecided because some of their facts could not be read. Subset.
	CannotRuleOut []string `yaml:"cannot_rule_out"`
	// RedundancyDegraded lists components the conclusion must report as
	// broken but covered by a redundancy group that holds. Subset.
	RedundancyDegraded []string `yaml:"redundancy_degraded"`
}

type Result struct {
	Scenario *Scenario
	Actual   Expectation
	Pass     bool
	// Err says why the scenario could not run, such as an entry the model
	// lacks; such a scenario fails.
	Err string
}
