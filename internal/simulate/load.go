// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
	if err := finish(&sc); err != nil {
		return nil, fmt.Errorf("scenario %q: %w", path, err)
	}
	return &sc, nil
}

// ParseScenarios reads one or more scenarios from data, separated by
// `---`, as an MCP client sends them inline.
func ParseScenarios(data []byte) ([]*Scenario, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var out []*Scenario
	for i := 1; ; i++ {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse scenario %d: %w", i, err)
		}
		// An empty document -- a leading or trailing `---` -- is not a
		// scenario that expects nothing.
		if len(doc.Content) == 0 || doc.Content[0].Kind == yaml.ScalarNode && doc.Content[0].Value == "" {
			i--
			continue
		}
		var sc Scenario
		if err := doc.Decode(&sc); err != nil {
			return nil, fmt.Errorf("parse scenario %d: %w", i, err)
		}
		if err := finish(&sc); err != nil {
			return nil, fmt.Errorf("scenario %d (%q): %w", i, sc.Name, err)
		}
		out = append(out, &sc)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no scenarios in the source")
	}
	return out, nil
}

// finish normalises a decoded scenario and checks its unresolved block.
// YAML decodes integers as int, but some environments produce float64:
// whole floats become int so the fact store and evaluator see int.
func finish(sc *Scenario) error {
	for comp := range sc.Inject {
		for k, v := range sc.Inject[comp] {
			sc.Inject[comp][k] = normaliseValue(v)
		}
	}
	return checkUnresolved(sc)
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
