package build

import (
	"fmt"
	"io"
	"sort"

	"github.com/mgt-tool/mgtt/internal/model"
	"gopkg.in/yaml.v3"
)

// EmitYAML writes model m to w as deterministic YAML:
//   - Top-level keys: meta, then components.
//   - meta fields: in struct-declaration order (stable).
//   - components: map keys sorted alphabetically.
//   - Each component's Depends list sorted by target component name.
//   - No timestamps, no generation metadata — the file is a function
//     of the model only.
//
// Running EmitYAML twice against the same *model.Model produces
// byte-identical output.
func EmitYAML(m *model.Model, w io.Writer) error {
	doc := buildDoc(m)
	node, err := toNode(doc)
	if err != nil {
		return fmt.Errorf("emit yaml: %w", err)
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	defer enc.Close()
	if err := enc.Encode(node); err != nil {
		return fmt.Errorf("emit yaml: %w", err)
	}
	return nil
}

// buildDoc converts *model.Model into the intermediate emitDoc,
// applying all sorting invariants.
func buildDoc(m *model.Model) *emitDoc {
	out := &emitDoc{
		Meta: emitMeta{
			Name:      m.Meta.Name,
			Version:   m.Meta.Version,
			Providers: append([]string(nil), m.Meta.Providers...),
		},
		Components: make(map[string]*emitComponent, len(m.Components)),
	}
	for name, c := range m.Components {
		ec := &emitComponent{
			Type:       c.Type,
			Providers:  append([]string(nil), c.Providers...),
			Resource:   c.Resource,
			HealthyRaw: append([]string(nil), c.HealthyRaw...),
		}
		for _, dep := range c.Depends {
			on := append([]string(nil), dep.On...)
			sort.Strings(on)
			ec.Depends = append(ec.Depends, emitDep{On: on})
		}
		sort.SliceStable(ec.Depends, func(i, j int) bool {
			a, b := "", ""
			if len(ec.Depends[i].On) > 0 {
				a = ec.Depends[i].On[0]
			}
			if len(ec.Depends[j].On) > 0 {
				b = ec.Depends[j].On[0]
			}
			return a < b
		})
		out.Components[name] = ec
	}
	return out
}

// toNode converts an emitDoc into a *yaml.Node with sorted component keys,
// guaranteeing byte-identical output across runs regardless of map iteration
// order. yaml.v3 emits map keys in insertion order, NOT sorted — so we must
// build the yaml.Node directly.
func toNode(doc *emitDoc) (*yaml.Node, error) {
	// Build meta node from struct (field order is declaration order — stable).
	metaNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendStrField(metaNode, "name", doc.Meta.Name)
	appendStrField(metaNode, "version", doc.Meta.Version)
	if len(doc.Meta.Providers) > 0 {
		metaNode.Content = append(metaNode.Content, strNode("providers"), strSeqNode(doc.Meta.Providers))
	}

	// Build components node with alphabetically sorted keys.
	compNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	names := make([]string, 0, len(doc.Components))
	for n := range doc.Components {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		ec := doc.Components[n]
		ecNode := componentNode(ec)
		compNode.Content = append(compNode.Content, strNode(n), ecNode)
	}

	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content, strNode("meta"), metaNode)
	root.Content = append(root.Content, strNode("components"), compNode)

	return root, nil
}

func componentNode(ec *emitComponent) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendStrField(n, "type", ec.Type)
	if len(ec.Providers) > 0 {
		n.Content = append(n.Content, strNode("providers"), strSeqNode(ec.Providers))
	}
	if ec.Resource != "" {
		appendStrField(n, "resource", ec.Resource)
	}
	if len(ec.HealthyRaw) > 0 {
		n.Content = append(n.Content, strNode("healthy"), strSeqNode(ec.HealthyRaw))
	}
	if len(ec.Depends) > 0 {
		depsNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, dep := range ec.Depends {
			depMap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			depMap.Content = append(depMap.Content, strNode("on"), strSeqNode(dep.On))
			depsNode.Content = append(depsNode.Content, depMap)
		}
		n.Content = append(n.Content, strNode("depends"), depsNode)
	}
	return n
}

func strNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
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

// Intermediate structs — we don't re-use model.Component directly because it
// carries compiled expr.Node fields that shouldn't round-trip through YAML.
type emitDoc struct {
	Meta       emitMeta
	Components map[string]*emitComponent
}

type emitMeta struct {
	Name      string
	Version   string
	Providers []string
}

type emitComponent struct {
	Type       string
	Providers  []string
	Resource   string
	HealthyRaw []string
	Depends    []emitDep
}

// emitDep serializes a Dependency's target list. We intentionally
// drop Dependency.WhileRaw (the `while:` key) here: generated models
// don't carry while-guards. while-guards are a hand-authored
// refinement; running `mgtt model build` against a model with
// while-guards would overwrite them. That risk is the operator's to
// manage (commit the generated model, review the diff) — not
// something the emitter silently round-trips.
type emitDep struct {
	On []string `yaml:"on"`
}
