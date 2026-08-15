// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package providersupport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgt-tool/mgtt/internal/expr"

	"gopkg.in/yaml.v3"
)

type rawProvider struct {
	Meta       rawMeta           `yaml:"meta"`
	Runtime    rawRuntime        `yaml:"runtime"`
	Install    rawInstall        `yaml:"install"`
	Variables  map[string]rawVar `yaml:"variables"`
	ReadOnly   *bool             `yaml:"read_only"`
	WritesNote string            `yaml:"writes_note"`
}

type rawMeta struct {
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description"`
	Tags        []string          `yaml:"tags"`
	Requires    map[string]string `yaml:"requires"`
}

type rawRuntime struct {
	// Needs and Backends accept either a YAML sequence (shorthand) or a
	// mapping (enriched). Decoded as yaml.Node so we can inspect Kind at
	// parse time and normalize to map[string]string.
	Needs       yaml.Node `yaml:"needs"`
	Backends    yaml.Node `yaml:"backends"`
	NetworkMode string    `yaml:"network_mode"`
	Entrypoint  string    `yaml:"entrypoint"`
}

type rawInstall struct {
	Source *rawInstallSource `yaml:"source"`
	Image  *rawInstallImage  `yaml:"image"`
}

type rawInstallSource struct {
	Build string `yaml:"build"`
	Clean string `yaml:"clean"`
}

type rawInstallImage struct {
	Repository string `yaml:"repository"`
}

type rawVar struct {
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Default     string `yaml:"default"`
}

type rawFact struct {
	Type  string   `yaml:"type"`
	TTL   string   `yaml:"ttl"`
	Probe rawProbe `yaml:"probe"`
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

// LoadFromBytes parses a provider YAML from a byte slice.
func LoadFromBytes(data []byte) (*Provider, error) {
	root, err := parseYAMLRoot(data)
	if err != nil {
		return nil, err
	}
	var raw rawProvider
	if err := root.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode provider metadata: %w", err)
	}
	p, err := providerFromRaw(raw)
	if err != nil {
		return nil, err
	}
	if err := validate(p); err != nil {
		return nil, err
	}
	if err := parseTypesNode(root, p); err != nil {
		return nil, err
	}
	return p, nil
}

// parseYAMLRoot unmarshals data and returns the root mapping node,
// unwrapping the document wrapper and rejecting empty / non-mapping
// inputs with a friendly error.
func parseYAMLRoot(data []byte) (*yaml.Node, error) {
	var docNode yaml.Node
	if err := yaml.Unmarshal(data, &docNode); err != nil {
		return nil, fmt.Errorf("provider YAML parse error: %w", err)
	}
	if docNode.Kind == 0 {
		return nil, fmt.Errorf("provider YAML is empty")
	}
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
	return root, nil
}

// providerFromRaw converts the YAML-decoded rawProvider into the typed
// Provider shape. Type-scanned body (root.Content[types]) is handled
// separately by parseTypesNode because yaml.v3 erases map ordering
// in rawProvider, which matters for state declaration order.
func providerFromRaw(raw rawProvider) (*Provider, error) {
	needs, err := decodeNameVersionMap(raw.Runtime.Needs, "runtime.needs")
	if err != nil {
		return nil, err
	}
	backends, err := decodeNameVersionMap(raw.Runtime.Backends, "runtime.backends")
	if err != nil {
		return nil, err
	}
	p := &Provider{
		Meta: ProviderMeta{
			Name:        raw.Meta.Name,
			Version:     raw.Meta.Version,
			Description: raw.Meta.Description,
			Tags:        raw.Meta.Tags,
			Requires:    raw.Meta.Requires,
		},
		Runtime: ProviderRuntime{
			Needs:       needs,
			Backends:    backends,
			NetworkMode: raw.Runtime.NetworkMode,
			Entrypoint:  raw.Runtime.Entrypoint,
		},
		Install: ProviderInstall{
			Source: toInstallSource(raw.Install.Source),
			Image:  toInstallImage(raw.Install.Image),
		},
		Types:      make(map[string]*Type),
		Variables:  make(map[string]Variable),
		ReadOnly:   true,
		WritesNote: raw.WritesNote,
	}
	if raw.ReadOnly != nil {
		p.ReadOnly = *raw.ReadOnly
	}
	for k, v := range raw.Variables {
		p.Variables[k] = Variable(v)
	}
	return p, nil
}

// parseTypesNode finds the "types:" mapping and parses each entry into
// p.Types. No-op when the document declares no types.
func parseTypesNode(root *yaml.Node, p *Provider) error {
	typesNode := mappingGet(root, "types")
	if typesNode == nil {
		return nil
	}
	if typesNode.Kind != yaml.MappingNode {
		return fmt.Errorf("'types' must be a mapping")
	}
	for i := 0; i+1 < len(typesNode.Content); i += 2 {
		typeName := typesNode.Content[i].Value
		t, err := parseType(typeName, typesNode.Content[i+1])
		if err != nil {
			return fmt.Errorf("type %q: %w", typeName, err)
		}
		p.Types[typeName] = t
	}
	return nil
}

// LoadFromFile reads a provider YAML file from disk.
func LoadFromFile(path string) (*Provider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read provider file %q: %w", path, err)
	}
	return LoadFromBytes(data)
}

// LoadTypeFromBytes parses a single type YAML document into a *Type. The
// document's top-level mapping matches the shape used in types/*.yaml files
// (description / facts / states / default_active_state / failure_modes).
// Callers are responsible for setting SourcePath afterwards if relevant.
func LoadTypeFromBytes(name string, data []byte) (*Type, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("type %q: YAML parse error: %w", name, err)
	}
	root := &doc
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return nil, fmt.Errorf("type %q: YAML document is empty", name)
		}
		root = root.Content[0]
	}
	return parseType(name, root)
}

// LoadFromDir loads a provider from a directory. It reads manifest.yaml for
// meta/needs/network/read_only/hooks/variables. If manifest.yaml contains an
// inline types: key, those are loaded (backward-compatible). Otherwise, it
// scans a types/ subdirectory and loads each .yaml file as a named type.
func LoadFromDir(dir string) (*Provider, error) {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read manifest.yaml in %q: %w", dir, err)
	}
	p, err := LoadFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse manifest.yaml in %q: %w", dir, err)
	}
	if len(p.Types) > 0 {
		return p, nil // inline types — backward-compatible path.
	}
	if err := loadTypesDir(filepath.Join(dir, "types"), p); err != nil {
		return nil, err
	}
	return p, nil
}

// loadTypesDir populates p.Types from every *.yaml file in typesDir.
// A missing directory is fine (provider with zero types); other read
// errors propagate.
func loadTypesDir(typesDir string, p *Provider) error {
	entries, err := os.ReadDir(typesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read types dir %q: %w", typesDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		typeName := strings.TrimSuffix(entry.Name(), ".yaml")
		typePath := filepath.Join(typesDir, entry.Name())
		t, err := loadTypeFile(typeName, typePath)
		if err != nil {
			return err
		}
		p.Types[typeName] = t
	}
	return nil
}

// loadTypeFile reads one types/<name>.yaml and hands the root mapping
// to parseType.
func loadTypeFile(typeName, typePath string) (*Type, error) {
	data, err := os.ReadFile(typePath)
	if err != nil {
		return nil, fmt.Errorf("read type file %q: %w", filepath.Base(typePath), err)
	}
	var typeNode yaml.Node
	if err := yaml.Unmarshal(data, &typeNode); err != nil {
		return nil, fmt.Errorf("type %q: YAML parse error: %w", typeName, err)
	}
	root := &typeNode
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return nil, fmt.Errorf("type %q: YAML document is empty", typeName)
		}
		root = root.Content[0]
	}
	t, err := parseType(typeName, root)
	if err != nil {
		return nil, fmt.Errorf("type %q: %w", typeName, err)
	}
	t.SourcePath = typePath
	return t, nil
}

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
	Healthy            any                    `yaml:"healthy"`
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
		HealthyRaw:         collectHealthyRaw(rf.Healthy),
	}
	if err := parseTypeFacts(rf.Facts, t); err != nil {
		return nil, err
	}
	if err := parseTypeStates(node, t); err != nil {
		return nil, err
	}
	for state, fm := range rf.FailureModes {
		t.FailureModes[state] = fm.CanCause
	}
	if err := compileTypeExpressions(name, t); err != nil {
		return nil, err
	}
	return t, nil
}

// collectHealthyRaw normalises the rf.Healthy YAML input (a single
// string OR a list of strings) into a []string.
func collectHealthyRaw(raw any) []string {
	switch v := raw.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func parseTypeFacts(rawFacts map[string]*rawFact, t *Type) error {
	for name, rawF := range rawFacts {
		fs, err := parseFact(rawF)
		if err != nil {
			return fmt.Errorf("fact %q: %w", name, err)
		}
		t.Facts[name] = fs
	}
	return nil
}

func parseTypeStates(node *yaml.Node, t *Type) error {
	statesNode := mappingGet(node, "states")
	if statesNode == nil {
		return nil
	}
	states, err := parseStatesOrdered(statesNode)
	if err != nil {
		return fmt.Errorf("parse states: %w", err)
	}
	t.States = states
	return nil
}

// compileTypeExpressions parses each raw expression string into an
// expr.Node. Healthy and state.When use the same grammar.
func compileTypeExpressions(typeName string, t *Type) error {
	for _, raw := range t.HealthyRaw {
		node, err := expr.Parse(raw)
		if err != nil {
			return fmt.Errorf("type %s: invalid healthy expression %q: %w", typeName, raw, err)
		}
		t.Healthy = append(t.Healthy, node)
	}
	for i := range t.States {
		s := &t.States[i]
		if s.WhenRaw == "" {
			continue
		}
		when, err := expr.Parse(s.WhenRaw)
		if err != nil {
			return fmt.Errorf("type %s: state %s: invalid when expression %q: %w", typeName, s.Name, s.WhenRaw, err)
		}
		s.When = when
	}
	return nil
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
			When        string   `yaml:"when"`
			Description string   `yaml:"description"`
			TriggeredBy []string `yaml:"triggered_by"`
		}
		if err := valNode.Decode(&rawState); err != nil {
			return nil, fmt.Errorf("state %q: %w", stateName, err)
		}

		states = append(states, StateDef{
			Name:        stateName,
			WhenRaw:     rawState.When,
			Description: rawState.Description,
			TriggeredBy: rawState.TriggeredBy,
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

// decodeNameVersionMap accepts either a YAML sequence of scalars
// (shorthand: [aws, kubectl]) or a mapping (enriched: {aws: ">=2.13"})
// and returns map[string]string with empty-string values for list-form
// entries that have no constraint.
func decodeNameVersionMap(n yaml.Node, fieldName string) (map[string]string, error) {
	out := map[string]string{}
	if n.Kind == 0 {
		return out, nil // absent key
	}
	switch n.Kind {
	case yaml.SequenceNode:
		for _, item := range n.Content {
			if item.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("%s: list form entries must be plain scalars; mixing list and map within one block is not allowed", fieldName)
			}
			out[item.Value] = ""
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			v := n.Content[i+1]
			if k.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("%s: keys must be scalars", fieldName)
			}
			if v.Kind == yaml.ScalarNode {
				out[k.Value] = v.Value
			} else {
				return nil, fmt.Errorf("%s: values must be scalars (semver range strings)", fieldName)
			}
		}
	default:
		return nil, fmt.Errorf("%s: must be a sequence or mapping", fieldName)
	}
	return out, nil
}

func toInstallSource(r *rawInstallSource) *InstallSource {
	if r == nil {
		return nil
	}
	return &InstallSource{Build: r.Build, Clean: r.Clean}
}

func toInstallImage(r *rawInstallImage) *InstallImage {
	if r == nil {
		return nil
	}
	return &InstallImage{Repository: r.Repository}
}

// validate enforces the v1.0 manifest invariants: install-method
// viability, network_mode enum, name convention, required meta fields.
func validate(p *Provider) error {
	if p.Meta.Name == "" {
		return fmt.Errorf("meta.name is required")
	}
	if !isValidShortname(p.Meta.Name) {
		return fmt.Errorf("meta.name %q must match [a-z][a-z0-9-]*", p.Meta.Name)
	}
	if p.Meta.Version == "" {
		return fmt.Errorf("meta.version is required")
	}
	if p.Meta.Description == "" {
		return fmt.Errorf("meta.description is required")
	}
	if p.Install.Source == nil && p.Install.Image == nil {
		return fmt.Errorf("install: at least one of source or image must be declared")
	}
	if p.Install.Source != nil {
		if p.Install.Source.Build == "" {
			return fmt.Errorf("install.source.build is required when source is declared")
		}
		if p.Install.Source.Clean == "" {
			return fmt.Errorf("install.source.clean is required when source is declared")
		}
	}
	switch p.Runtime.NetworkMode {
	case "", "bridge", "host":
	default:
		return fmt.Errorf("runtime.network_mode %q: must be \"bridge\" or \"host\"", p.Runtime.NetworkMode)
	}
	if !p.ReadOnly && strings.TrimSpace(p.WritesNote) == "" {
		return fmt.Errorf("read_only: false requires a non-empty writes_note")
	}
	return nil
}

func isValidShortname(s string) bool {
	if s == "" {
		return false
	}
	if !(s[0] >= 'a' && s[0] <= 'z') {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}
