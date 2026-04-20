package strategy

import (
	"sort"
)

type bfsStrategy struct{}

// BFS returns the graph-traversal strategy (the pre-scenarios engine
// behavior). Fallback when no scenarios are available.
func BFS() Strategy { return bfsStrategy{} }

func (bfsStrategy) Name() string { return "bfs" }

func (s bfsStrategy) SuggestProbe(in Input) Decision {
	if in.Model == nil {
		return Decision{Stuck: true, Reason: "no model"}
	}

	// BFS from entry point through dependency graph; return first
	// uncollected fact.
	entry := in.Model.EntryPoint()
	if entry == "" {
		return Decision{Stuck: true, Reason: "model has no entry point"}
	}
	visited := map[string]bool{}
	queue := []string{entry}
	var candidates []string
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if visited[c] {
			continue
		}
		visited[c] = true
		candidates = append(candidates, c)
		comp := in.Model.Components[c]
		if comp == nil {
			continue
		}
		for _, dep := range comp.Depends {
			for _, target := range dep.On {
				if !visited[target] {
					queue = append(queue, target)
				}
			}
		}
	}

	for _, compName := range candidates {
		comp := in.Model.Components[compName]
		if comp == nil {
			continue
		}
		providers := comp.Providers
		if len(providers) == 0 {
			providers = in.Model.Meta.Providers
		}
		if in.Registry == nil {
			continue
		}
		t, providerName, err := in.Registry.ResolveType(providers, comp.Type)
		if err != nil || t == nil {
			continue
		}
		names := make([]string, 0, len(t.Facts))
		for n := range t.Facts {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, fn := range names {
			if in.Store != nil && in.Store.Latest(compName, fn) != nil {
				continue
			}
			fs := t.Facts[fn]
			if fs == nil {
				continue
			}
			return Decision{Probe: &Probe{
				Component: compName,
				Fact:      fn,
				Provider:  providerName,
				Type:      t.Name,
				Resource:  comp.Resource,
				Cost:      fs.Probe.Cost,
				Access:    fs.Probe.Access,
				Command:   fs.Probe.Cmd,
				ParseMode: fs.Probe.Parse,
				Vars:      componentVars(in, compName),
			}}
		}
	}
	// BFS has probed every reachable fact. Before declaring "all
	// healthy", check whether any component is standalone unhealthy
	// (its healthy predicate is definitively false) — otherwise a
	// broken upstream with no observed downstream symptom gets
	// silently reported as healthy.
	if root := standaloneUnhealthyRoot(in); root != nil {
		return Decision{Done: true, RootCause: root, Reason: "standalone unhealthy component"}
	}
	return Decision{Done: true, Reason: "bfs coverage exhausted"}
}
