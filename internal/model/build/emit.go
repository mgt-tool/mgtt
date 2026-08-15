// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package build

import (
	"fmt"
	"io"
	"sort"

	"github.com/mgt-tool/mgtt/internal/model"
	"gopkg.in/yaml.v3"
)

// EmitYAML writes model m to w as deterministic YAML. Output shape:
//   - Top-level: meta, then components.
//   - meta: name, version, providers, vars, strict_types, scenarios
//     (in that order, omitempty for zero values).
//   - components: keys sorted alphabetically. Each component's Depends
//     list sorted by first target; On list sorted within each dep.
//   - Every hand-authored field that Load preserves — HealthyRaw,
//     FailureModes, Vars, WhileRaw — is round-tripped, so a
//     Load→EmitYAML cycle is lossless.
//
// No timestamps, no generation metadata — the file is a pure function
// of *Model. Running EmitYAML twice against the same model produces
// byte-identical output.
func EmitYAML(m *model.Model, w io.Writer) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(modelNode(m)); err != nil {
		_ = enc.Close()
		return fmt.Errorf("emit yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("emit yaml: close encoder: %w", err)
	}
	return nil
}

// modelNode builds the root YAML document directly from *model.Model,
// guaranteeing deterministic output via sorted component keys and
// sorted depends targets. yaml.v3 emits map keys in insertion order,
// NOT sorted — so we must build the yaml.Node tree by hand.
func modelNode(m *model.Model) *yaml.Node {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content,
		strNode("meta"), metaNode(m.Meta),
		strNode("components"), componentsNode(m),
	)
	return root
}

func metaNode(meta model.Meta) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendStrField(n, "name", meta.Name)
	appendStrField(n, "version", meta.Version)
	if len(meta.Providers) > 0 {
		n.Content = append(n.Content, strNode("providers"), strSeqNode(meta.Providers))
	}
	if len(meta.Vars) > 0 {
		n.Content = append(n.Content, strNode("vars"), stringMapNode(meta.Vars))
	}
	if meta.StrictTypes {
		n.Content = append(n.Content, strNode("strict_types"), boolNode(true))
	}
	if meta.Scenarios != "" {
		appendStrField(n, "scenarios", meta.Scenarios)
	}
	return n
}

func componentsNode(m *model.Model) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	names := make([]string, 0, len(m.Components))
	for name := range m.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		n.Content = append(n.Content, strNode(name), componentNode(m.Components[name]))
	}
	return n
}

func componentNode(c *model.Component) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendStrField(n, "type", c.Type)
	if len(c.Providers) > 0 {
		n.Content = append(n.Content, strNode("providers"), strSeqNode(c.Providers))
	}
	if c.Resource != "" {
		appendStrField(n, "resource", c.Resource)
	}
	if len(c.Vars) > 0 {
		n.Content = append(n.Content, strNode("vars"), stringMapNode(c.Vars))
	}
	if len(c.HealthyRaw) > 0 {
		n.Content = append(n.Content, strNode("healthy"), strSeqNode(c.HealthyRaw))
	}
	if deps := dependsNode(c.Depends); deps != nil {
		n.Content = append(n.Content, strNode("depends"), deps)
	}
	if fm := failureModesNode(c.FailureModes); fm != nil {
		n.Content = append(n.Content, strNode("failure_modes"), fm)
	}
	return n
}

func dependsNode(deps []model.Dependency) *yaml.Node {
	if len(deps) == 0 {
		return nil
	}
	sorted := make([]model.Dependency, len(deps))
	copy(sorted, deps)
	for i := range sorted {
		on := append([]string(nil), sorted[i].On...)
		sort.Strings(on)
		sorted[i].On = on
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := "", ""
		if len(sorted[i].On) > 0 {
			a = sorted[i].On[0]
		}
		if len(sorted[j].On) > 0 {
			b = sorted[j].On[0]
		}
		return a < b
	})
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, dep := range sorted {
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		m.Content = append(m.Content, strNode("on"), strSeqNode(dep.On))
		if dep.WhileRaw != "" {
			appendStrField(m, "while", dep.WhileRaw)
		}
		seq.Content = append(seq.Content, m)
	}
	return seq
}

func failureModesNode(fm map[string][]string) *yaml.Node {
	if len(fm) == 0 {
		return nil
	}
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	states := make([]string, 0, len(fm))
	for s := range fm {
		states = append(states, s)
	}
	sort.Strings(states)
	for _, state := range states {
		body := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		body.Content = append(body.Content, strNode("can_cause"), strSeqNode(fm[state]))
		n.Content = append(n.Content, strNode(state), body)
	}
	return n
}

func stringMapNode(m map[string]string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		appendStrField(n, k, m[k])
	}
	return n
}

func strNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func boolNode(b bool) *yaml.Node {
	v := "false"
	if b {
		v = "true"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: v}
}

func appendStrField(parent *yaml.Node, key, val string) {
	parent.Content = append(parent.Content, strNode(key), strNode(val))
}

func strSeqNode(vals []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, v := range vals {
		n.Content = append(n.Content, strNode(v))
	}
	return n
}
