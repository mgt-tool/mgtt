package model

import (
	"fmt"
	"os"
	"sort"

	"github.com/sajonaro/mgtt/internal/expr"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Raw YAML structures (unexported)
// ---------------------------------------------------------------------------

// rawModel is the top-level YAML structure.
type rawModel struct {
	Meta       rawMeta                   `yaml:"meta"`
	Components map[string]*rawComponent  `yaml:"components"`
}

type rawMeta struct {
	Name      string            `yaml:"name"`
	Version   string            `yaml:"version"`
	Providers []string          `yaml:"providers"`
	Vars      map[string]string `yaml:"vars"`
}

// rawComponent mirrors the YAML component block.
type rawComponent struct {
	Type         string                   `yaml:"type"`
	Providers    []string                 `yaml:"providers"`
	Depends      []rawDependency          `yaml:"depends"`
	Healthy      []string                 `yaml:"healthy"`
	FailureModes map[string]rawFailMode   `yaml:"failure_modes"`

	// yamlLine is populated after parsing by scanning the raw document node.
	yamlLine int
}

// rawDependency mirrors depends list entries.
// The "on" field can be a scalar string or a list of strings.
type rawDependency struct {
	OnRaw    interface{} `yaml:"on"`
	WhileRaw string      `yaml:"while"`
}

type rawFailMode struct {
	CanCause []string `yaml:"can_cause"`
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

// Load reads the YAML file at path, parses it into a Model, and builds the
// internal dependency graph.
func Load(path string) (*Model, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("model: read %q: %w", path, err)
	}

	// First pass: parse into a yaml.Node so we can extract declaration order
	// (byte offsets of component keys).
	var docNode yaml.Node
	if err := yaml.Unmarshal(data, &docNode); err != nil {
		return nil, fmt.Errorf("model: parse %q: %w", path, err)
	}

	// Second pass: decode into the typed raw structs.
	var raw rawModel
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("model: decode %q: %w", path, err)
	}

	if raw.Components == nil {
		raw.Components = make(map[string]*rawComponent)
	}

	// Determine declaration order by extracting line numbers from the document
	// node, then sorting by line number.
	order := extractOrder(&docNode, raw.Components)

	// Build the typed Model.
	m := &Model{
		Meta: Meta{
			Name:      raw.Meta.Name,
			Version:   raw.Meta.Version,
			Providers: raw.Meta.Providers,
			Vars:      raw.Meta.Vars,
		},
		Components: make(map[string]*Component, len(raw.Components)),
		Order:      order,
	}

	for name, rc := range raw.Components {
		comp := &Component{
			Name:       name,
			Type:       rc.Type,
			Providers:  rc.Providers,
			HealthyRaw: rc.Healthy,
		}

		// failure_modes: map state → can_cause
		if len(rc.FailureModes) > 0 {
			comp.FailureModes = make(map[string][]string, len(rc.FailureModes))
			for state, fm := range rc.FailureModes {
				comp.FailureModes[state] = fm.CanCause
			}
		}

		// healthy: compile each raw expression string into an expr.Node
		for _, raw := range rc.Healthy {
			node, err := expr.Parse(raw)
			if err != nil {
				return nil, fmt.Errorf("component %s: invalid healthy expression %q: %w", name, raw, err)
			}
			comp.Healthy = append(comp.Healthy, node)
		}

		// depends: normalise rawDependency.OnRaw (string or []interface{})
		for _, rd := range rc.Depends {
			dep := Dependency{
				WhileRaw: rd.WhileRaw,
				On:       normaliseOn(rd.OnRaw),
			}
			if dep.WhileRaw != "" {
				w, err := expr.Parse(dep.WhileRaw)
				if err != nil {
					return nil, fmt.Errorf("component %s: invalid while expression %q: %w", name, dep.WhileRaw, err)
				}
				dep.While = w
			}
			comp.Depends = append(comp.Depends, dep)
		}

		m.Components[name] = comp
	}

	// Build the dependency graph.
	m.BuildGraph()

	return m, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// normaliseOn converts the raw "on" value (which may be a string or
// []interface{}) into []string.
func normaliseOn(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case string:
		return []string{t}
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// extractOrder walks the yaml.Node document tree to find the line numbers of
// each component key under the "components" mapping, then returns component
// names sorted by line number.
func extractOrder(doc *yaml.Node, components map[string]*rawComponent) []string {
	lineMap := make(map[string]int, len(components))

	// A valid YAML document has doc.Kind == yaml.DocumentNode with one child
	// which is a MappingNode.
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return sortedKeys(components)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return sortedKeys(components)
	}

	// Find the "components" mapping.
	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		valNode := root.Content[i+1]
		if keyNode.Value == "components" && valNode.Kind == yaml.MappingNode {
			// Each pair in valNode.Content is (componentName, componentBody).
			for j := 0; j+1 < len(valNode.Content); j += 2 {
				compKeyNode := valNode.Content[j]
				lineMap[compKeyNode.Value] = compKeyNode.Line
			}
			break
		}
	}

	// Fall back to sorted keys for any component missing from the scan.
	names := make([]string, 0, len(components))
	for name := range components {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		li := lineMap[names[i]]
		lj := lineMap[names[j]]
		if li != lj {
			return li < lj
		}
		return names[i] < names[j]
	})
	return names
}

// sortedKeys returns the keys of the map in alphabetical order (fallback).
func sortedKeys(m map[string]*rawComponent) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
