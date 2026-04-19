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
// promoted to an FQN for subgraph grouping. Pure function — no file
// I/O, no goroutines, no globals.
func Render(m *Model, reg *providersupport.Registry, installed []InstalledProvider) (string, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s — dependency graph\n\n", m.Meta.Name)
	sb.WriteString("```mermaid\ngraph LR\n")

	names := sortedComponentNames(m)

	// Resolve each component's owning-provider FQN.
	componentsByFQN := make(map[string][]string)
	for _, n := range names {
		fqn := resolveFQN(m, reg, installed, n)
		componentsByFQN[fqn] = append(componentsByFQN[fqn], n)
	}

	// Decide flat vs grouped.
	uniqueFQNs := make([]string, 0, len(componentsByFQN))
	for f := range componentsByFQN {
		uniqueFQNs = append(uniqueFQNs, f)
	}
	sort.Slice(uniqueFQNs, func(i, j int) bool {
		// "generic" sorts last; everything else alphabetical.
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
	for _, fqn := range uniqueFQNs {
		if !flat {
			fmt.Fprintf(&sb, "  subgraph %s [%q]\n", fqnID(fqn), fqn)
		}
		for _, n := range componentsByFQN[fqn] {
			c := m.Components[n]
			label := fmt.Sprintf("%s<br/>%s", n, c.Type)
			openBracket, closeBracket := shapeFor(c.Type)
			indent := "  "
			if !flat {
				indent = "    "
			}
			fmt.Fprintf(&sb, "%s%s%s%q%s\n", indent, n, openBracket, label, closeBracket)
		}
		if !flat {
			sb.WriteString("  end\n")
		}
	}

	// Edges, sorted by (from, to).
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
		fmt.Fprintf(&sb, "  %s --> %s\n", e.from, e.to)
	}

	sb.WriteString("```\n")
	return sb.String(), nil
}

// resolveFQN returns the owning-provider FQN for component name, or
// "generic" if the type falls back to the generic provider.
func resolveFQN(m *Model, reg *providersupport.Registry, installed []InstalledProvider, name string) string {
	c := m.Components[name]
	providers := c.Providers
	if len(providers) == 0 {
		providers = m.Meta.Providers
	}
	_, owner, err := reg.ResolveType(providers, c.Type)
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
	r := strings.NewReplacer("/", "_", "-", "_", "@", "_", ".", "_")
	return r.Replace(fqn)
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
