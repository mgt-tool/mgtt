// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package facts

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// factYAML is the YAML representation of a Fact.
type factYAML struct {
	Key       string     `yaml:"key"`
	Value     any        `yaml:"value"`
	Collector string     `yaml:"collector"`
	At        time.Time  `yaml:"at"`
	Note      string     `yaml:"note,omitempty"`
	Raw       string     `yaml:"raw,omitempty"`
	Status    FactStatus `yaml:"status,omitempty"`
}

// storeYAML is the top-level YAML structure for a state file.
type storeYAML struct {
	Meta  StoreMeta             `yaml:"meta"`
	Facts map[string][]factYAML `yaml:"facts"`
}

// NewDiskBacked creates a store backed by a file path.
// Call Save() after each Append to persist.
func NewDiskBacked(path string, meta StoreMeta) *Store {
	return &Store{
		facts: make(map[string][]Fact),
		path:  path,
		Meta:  meta,
	}
}

// Load reads a state.yaml file into a Store.
func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var sy storeYAML
	if err := yaml.Unmarshal(data, &sy); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	s := &Store{
		facts: make(map[string][]Fact),
		path:  path,
		Meta:  sy.Meta,
	}

	for component, yFacts := range sy.Facts {
		for _, yf := range yFacts {
			s.facts[component] = append(s.facts[component], Fact(yf))
		}
	}

	return s, nil
}

// Save writes the store to disk using atomic rename (write .tmp, rename).
func (s *Store) Save() error {
	if s.path == "" {
		return fmt.Errorf("store has no path: use NewDiskBacked to create a disk-backed store")
	}

	sy := storeYAML{
		Meta:  s.Meta,
		Facts: make(map[string][]factYAML, len(s.facts)),
	}
	for component, facts := range s.facts {
		yFacts := make([]factYAML, len(facts))
		for i, f := range facts {
			yFacts[i] = factYAML(f)
		}
		sy.Facts[component] = yFacts
	}

	data, err := yaml.Marshal(sy)
	if err != nil {
		return fmt.Errorf("marshaling store: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing tmp file %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("renaming %s -> %s: %w", tmp, s.path, err)
	}

	return nil
}

// AppendAndSave appends a fact and saves to disk.
func (s *Store) AppendAndSave(component string, f Fact) error {
	s.Append(component, f)
	return s.Save()
}
