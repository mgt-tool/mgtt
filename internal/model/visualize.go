// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// Render emits the markdown+mermaid body for m. The registry is used to
// resolve each component's type to its owning provider (bare name); the
// installed list supplies the Namespace so the owning name can be
// promoted to an FQN for subgraph grouping. No file I/O, no goroutines,
// no globals — but does lazily populate m.graph via BuildGraph() when
// nil, same pattern as EntryPoint and DependenciesOf on *Model.
func Render(m *Model, reg *providersupport.Registry, installed []InstalledProvider) (string, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s — dependency graph\n\n", m.Meta.Name)
	sb.WriteString("```mermaid\ngraph LR\n")

	// Cycle detection: emit a warning comment but keep rendering — the
	// warning tells operators the layout may be unstable; mermaid /
	// mkdocs still parse fine. Guard against Model literals built by
	// tests without BuildGraph.
	if m.graph == nil {
		m.BuildGraph()
	}
	if cycle := m.graph.DetectCycle(); cycle != nil {
		fmt.Fprintf(&sb, "  %%%% warning: cycle detected (%s) — mermaid layout may be unstable\n",
			strings.Join(cycle, " -> "))
	}

	names := sortedComponentNames(m)
	if len(names) == 0 {
		sb.WriteString("  %% no components\n")
		sb.WriteString("```\n")
		return sb.String(), nil
	}

	renderComponentNodes(&sb, m, reg, installed, names)
	renderEdges(&sb, m, names)
	sb.WriteString("```\n")
	return sb.String(), nil
}

// renderComponentNodes writes every component as a mermaid node,
// grouping them into subgraphs when more than one owning-provider FQN
// is present.
func renderComponentNodes(sb *strings.Builder, m *Model, reg *providersupport.Registry, installed []InstalledProvider, names []string) {
	componentsByFQN := map[string][]string{}
	for _, n := range names {
		fqn := resolveFQN(m, reg, installed, n)
		componentsByFQN[fqn] = append(componentsByFQN[fqn], n)
	}
	uniqueFQNs := make([]string, 0, len(componentsByFQN))
	for f := range componentsByFQN {
		uniqueFQNs = append(uniqueFQNs, f)
	}
	// "generic" sorts last so author-specific providers appear first.
	sort.Slice(uniqueFQNs, func(i, j int) bool {
		a, b := uniqueFQNs[i], uniqueFQNs[j]
		if a == "generic" {
			return false
		}
		if b == "generic" {
			return true
		}
		return a < b
	})

	flat := len(uniqueFQNs) <= 1
	indent := "  "
	if !flat {
		indent = "    "
	}
	for _, fqn := range uniqueFQNs {
		if !flat {
			fmt.Fprintf(sb, "  subgraph %s [%q]\n", fqnID(fqn), fqn)
		}
		for _, n := range componentsByFQN[fqn] {
			c := m.Components[n]
			openB, closeB := shapeFor(c.Type)
			fmt.Fprintf(sb, "%s%s%s%q%s\n", indent, nodeID(n), openB, nodeLabel(n, c), closeB)
		}
		if !flat {
			sb.WriteString("  end\n")
		}
	}
}

// nodeLabel returns the mermaid node label for a component — "name<br/>type",
// with an optional "→ resource" row when the resource differs from the name.
func nodeLabel(n string, c *Component) string {
	label := n + "<br/>" + c.Type
	if c.Resource != "" && c.Resource != n {
		label += "<br/>→ " + c.Resource
	}
	return label
}

// renderEdges emits a sorted list of `a --> b` lines for every depends
// edge in the model.
func renderEdges(sb *strings.Builder, m *Model, names []string) {
	type edge struct{ from, to string }
	var edges []edge
	for _, n := range names {
		c := m.Components[n]
		for _, d := range c.Depends {
			for _, target := range d.On {
				edges = append(edges, edge{from: n, to: target})
			}
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].from != edges[j].from {
			return edges[i].from < edges[j].from
		}
		return edges[i].to < edges[j].to
	})
	for _, e := range edges {
		fmt.Fprintf(sb, "  %s --> %s\n", nodeID(e.from), nodeID(e.to))
	}
}

// resolveFQN returns the owning-provider FQN for component name, or
// "generic" if the type falls back to the generic provider.
func resolveFQN(m *Model, reg *providersupport.Registry, installed []InstalledProvider, name string) string {
	c := m.Components[name]
	_, owner, err := c.ResolveType(m, reg)
	if err != nil || owner == "" || owner == providersupport.GenericProviderName {
		return "generic"
	}
	for _, ip := range installed {
		if ip.Name == owner {
			if ip.Namespace != "" {
				return ip.Namespace + "/" + ip.Name
			}
			return ip.Name
		}
	}
	return owner
}

// fqnID turns a FQN like "mgt-tool/kubernetes" into a mermaid-safe
// identifier "mgt_tool_kubernetes" ([a-zA-Z0-9_] only).
func fqnID(fqn string) string {
	return sanitizeForMermaidID(fqn)
}

// nodeID turns a component name into a mermaid-safe node id. The
// readable name stays in the label (inside quotes); this is only for
// the id mermaid parses — which must match [A-Za-z0-9_] reliably across
// renderers. Component keys in real models contain `/` (SSM parameter
// paths), `-` (k8s resource names), and `.` / `@` (FQN-style keys);
// none of those parse as ids, so we normalize to `_`.
func nodeID(name string) string {
	return sanitizeForMermaidID(name)
}

func sanitizeForMermaidID(s string) string {
	r := strings.NewReplacer("/", "_", "-", "_", "@", "_", ".", "_")
	return r.Replace(s)
}

func sortedComponentNames(m *Model) []string {
	names := make([]string, 0, len(m.Components))
	for n := range m.Components {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// shapeFor returns the mermaid bracket pair for a given component type.
// Match order is intentional: DB patterns win over generic substrings
// (e.g. "cache" in "elasticache" shouldn't be caught by something more
// general). First match wins.
func shapeFor(typ string) (openBracket, closeBracket string) {
	t := strings.ToLower(typ)
	switch {
	case containsAny(t, "bucket", "rds", "database", "cache", "elasticache"):
		return "[(", ")]"
	case containsAny(t, "broker", "queue"):
		return "[/", "\\]"
	case containsAny(t, "cdn", "ingress"):
		return "([", "])"
	default:
		return "[", "]"
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
