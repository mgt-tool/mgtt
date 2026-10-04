// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/mgt-tool/mgtt/internal/facts"
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

	if err := checkUnresolved(&sc); err != nil {
		return nil, fmt.Errorf("scenario %q: %w", path, err)
	}
	return &sc, nil
}

// unresolvedStatuses are the outcomes an unresolved: entry may name — the
// fact statuses a probe records when it ran but produced no value.
var unresolvedStatuses = map[string]facts.FactStatus{
	"not_found": facts.FactStatusNotFound,
	"forbidden": facts.FactStatusForbidden,
	"transient": facts.FactStatusTransient,
}

// checkUnresolved rejects an unknown status, and a fact given both a value
// and a probe failure, which no single probe can produce.
func checkUnresolved(sc *Scenario) error {
	for comp, kvs := range sc.Unresolved {
		for fact, status := range kvs {
			if _, ok := unresolvedStatuses[status]; !ok {
				return fmt.Errorf("unresolved: %s.%s: status %q (want not_found, forbidden or transient)", comp, fact, status)
			}
			if _, both := sc.Inject[comp][fact]; both {
				return fmt.Errorf("%s.%s is both injected and unresolved", comp, fact)
			}
		}
	}
	return nil
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
