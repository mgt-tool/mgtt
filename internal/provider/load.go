package provider

import (
	"fmt"
	"os"
	"time"

	"mgtt/internal/expr"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Raw intermediate structs for YAML decoding (sans types, which need
// special treatment for state ordering).
// ---------------------------------------------------------------------------

type rawProvider struct {
	Meta      rawMeta           `yaml:"meta"`
	Variables map[string]rawVar `yaml:"variables"`
	Auth      rawAuth           `yaml:"auth"`
	// Types intentionally omitted — parsed manually from yaml.Node tree.
}

type rawMeta struct {
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description"`
	Requires    map[string]string `yaml:"requires"`
	Runner      string            `yaml:"runner"`
}

type rawVar struct {
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Default     string `yaml:"default"`
}

type rawAuth struct {
	Strategy  string   `yaml:"strategy"`
	ReadsFrom []string `yaml:"reads_from"`
	Access    struct {
		Probes string `yaml:"probes"`
		Writes string `yaml:"writes"`
	} `yaml:"access"`
}

type rawFact struct {
	Type    string      `yaml:"type"`
	TTL     string      `yaml:"ttl"`
	Default interface{} `yaml:"default"`
	Probe   rawProbe    `yaml:"probe"`
}

type rawProbe struct {
	Cmd     string `yaml:"cmd"`
	Parse   string `yaml:"parse"`
	Cost    string `yaml:"cost"`
	Access  string `yaml:"access"`
	Timeout string `yaml:"timeout"`
}

type rawFailMode struct {
	CanCause []string `yaml:"can_cause"`
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// LoadFromBytes parses a provider YAML from a byte slice.
func LoadFromBytes(data []byte) (*Provider, error) {
	// Decode into a generic yaml.Node first so we can traverse the tree.
	var docNode yaml.Node
	if err := yaml.Unmarshal(data, &docNode); err != nil {
		return nil, fmt.Errorf("provider YAML parse error: %w", err)
	}
	if docNode.Kind == 0 {
		return nil, fmt.Errorf("provider YAML is empty")
	}

	// Unwrap DocumentNode → actual MappingNode.
	root := &docNode
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return nil, fmt.Errorf("provider YAML document is empty")
		}
		root = root.Content[0]
	}

	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("provider YAML root must be a mapping")
	}

	// Decode the raw non-types fields.
	var raw rawProvider
	if err := root.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode provider metadata: %w", err)
	}

	p := &Provider{
		Meta: ProviderMeta{
			Name:        raw.Meta.Name,
			Version:     raw.Meta.Version,
			Description: raw.Meta.Description,
			Requires:    raw.Meta.Requires,
			Runner:      raw.Meta.Runner,
		},
		DataTypes: make(map[string]DataType),
		Types:     make(map[string]*Type),
		Variables: make(map[string]Variable),
		Auth: AuthSpec{
			Strategy:  raw.Auth.Strategy,
			ReadsFrom: raw.Auth.ReadsFrom,
			Access: AuthAccess{
				Probes: raw.Auth.Access.Probes,
				Writes: raw.Auth.Access.Writes,
			},
		},
	}

	for k, v := range raw.Variables {
		p.Variables[k] = Variable{
			Description: v.Description,
			Required:    v.Required,
			Default:     v.Default,
		}
	}

	// Find the "types" key in the root mapping and parse types from its node.
	typesNode := mappingGet(root, "types")
	if typesNode != nil {
		if typesNode.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("'types' must be a mapping")
		}
		for i := 0; i+1 < len(typesNode.Content); i += 2 {
			keyNode := typesNode.Content[i]
			valNode := typesNode.Content[i+1]
			typeName := keyNode.Value
			t, err := parseType(typeName, valNode)
			if err != nil {
				return nil, fmt.Errorf("type %q: %w", typeName, err)
			}
			p.Types[typeName] = t
		}
	}

	return p, nil
}

// LoadFromFile reads a provider YAML file from disk.
func LoadFromFile(path string) (*Provider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read provider file %q: %w", path, err)
	}
	return LoadFromBytes(data)
}

// ---------------------------------------------------------------------------
// Internal parsing helpers
// ---------------------------------------------------------------------------

// mappingGet returns the value node for a key within a MappingNode, or nil.
func mappingGet(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// rawTypeFields holds typed results decoded from a type node.
type rawTypeFields struct {
	Description        string                 `yaml:"description"`
	Healthy            interface{}            `yaml:"healthy"`
	DefaultActiveState string                 `yaml:"default_active_state"`
	FailureModes       map[string]rawFailMode `yaml:"failure_modes"`
	Facts              map[string]*rawFact    `yaml:"facts"`
}

// parseType converts a yaml.Node representing a type definition into a *Type.
// The node is the value node of a type entry in the types mapping.
func parseType(name string, node *yaml.Node) (*Type, error) {
	var rf rawTypeFields
	if err := node.Decode(&rf); err != nil {
		return nil, fmt.Errorf("decode type fields: %w", err)
	}

	t := &Type{
		Name:               name,
		Description:        rf.Description,
		Facts:              make(map[string]*FactSpec),
		DefaultActiveState: rf.DefaultActiveState,
		FailureModes:       make(map[string][]string),
	}

	// Parse facts.
	for factName, rawF := range rf.Facts {
		fs, err := parseFact(rawF)
		if err != nil {
			return nil, fmt.Errorf("fact %q: %w", factName, err)
		}
		t.Facts[factName] = fs
	}

	// Parse healthy conditions.
	switch v := rf.Healthy.(type) {
	case string:
		t.HealthyRaw = []string{v}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				t.HealthyRaw = append(t.HealthyRaw, s)
			}
		}
	}

	// Parse states in YAML declaration order using the raw yaml.Node.
	statesNode := mappingGet(node, "states")
	if statesNode != nil {
		states, err := parseStatesOrdered(statesNode)
		if err != nil {
			return nil, fmt.Errorf("parse states: %w", err)
		}
		t.States = states
	}

	// Parse failure modes.
	for stateName, fm := range rf.FailureModes {
		t.FailureModes[stateName] = fm.CanCause
	}

	// Compile healthy expressions.
	for _, raw := range t.HealthyRaw {
		node, err := expr.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("type %s: invalid healthy expression %q: %w", name, raw, err)
		}
		t.Healthy = append(t.Healthy, node)
	}

	// Compile state when-expressions.
	for i := range t.States {
		if t.States[i].WhenRaw != "" {
			when, err := expr.Parse(t.States[i].WhenRaw)
			if err != nil {
				return nil, fmt.Errorf("type %s: state %s: invalid when expression %q: %w", name, t.States[i].Name, t.States[i].WhenRaw, err)
			}
			t.States[i].When = when
		}
	}

	return t, nil
}

// parseStatesOrdered extracts StateDef entries from a MappingNode in
// declaration order. This is critical: state evaluation order must match
// the order the author wrote them.
func parseStatesOrdered(node *yaml.Node) ([]StateDef, error) {
	n := node
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("states must be a mapping, got kind %v", n.Kind)
	}

	var states []StateDef
	// MappingNode.Content is [key0, val0, key1, val1, ...].
	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode := n.Content[i]
		valNode := n.Content[i+1]

		stateName := keyNode.Value

		var rawState struct {
			When        string `yaml:"when"`
			Description string `yaml:"description"`
		}
		if err := valNode.Decode(&rawState); err != nil {
			return nil, fmt.Errorf("state %q: %w", stateName, err)
		}

		states = append(states, StateDef{
			Name:        stateName,
			WhenRaw:     rawState.When,
			Description: rawState.Description,
		})
	}

	return states, nil
}

// parseFact converts a rawFact into a *FactSpec.
func parseFact(rf *rawFact) (*FactSpec, error) {
	if rf == nil {
		return nil, fmt.Errorf("nil fact spec")
	}

	fs := &FactSpec{
		TypeName: rf.Type,
		Default:  rf.Default,
		Probe: ProbeDef{
			Cmd:    rf.Probe.Cmd,
			Parse:  rf.Probe.Parse,
			Cost:   rf.Probe.Cost,
			Access: rf.Probe.Access,
		},
	}

	if rf.TTL != "" {
		d, err := time.ParseDuration(rf.TTL)
		if err != nil {
			return nil, fmt.Errorf("ttl %q: %w", rf.TTL, err)
		}
		fs.TTL = d
	}

	if rf.Probe.Timeout != "" {
		d, err := time.ParseDuration(rf.Probe.Timeout)
		if err != nil {
			return nil, fmt.Errorf("probe timeout %q: %w", rf.Probe.Timeout, err)
		}
		fs.Probe.Timeout = d
	}

	return fs, nil
}
