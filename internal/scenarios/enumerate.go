package scenarios

import (
	"fmt"
	"sort"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// Enumerate walks the model + type registry and emits every plausible
// failure-chain scenario. Output is deterministic and IDs assigned
// s-0001, s-0002, ... by sort position.
//
// Permissive default: when a downstream state has no TriggeredBy
// declaration, it accepts any upstream can_cause label.
func Enumerate(m *model.Model, reg *providersupport.Registry) []Scenario {
	var out []Scenario

	// Iterate components in a deterministic order.
	compNames := make([]string, 0, len(m.Components))
	for name := range m.Components {
		compNames = append(compNames, name)
	}
	sort.Strings(compNames)

	for _, compName := range compNames {
		comp := m.Components[compName]
		providers := effectiveProviders(comp, m)
		t, _, err := reg.ResolveType(providers, comp.Type)
		if err != nil || t == nil {
			continue
		}
		for _, state := range t.States {
			if state.Name == t.DefaultActiveState {
				continue
			}
			chains := extendChain(m, reg, compName, state, map[string]bool{})
			for _, chain := range chains {
				out = append(out, Scenario{
					Root:  RootRef{Component: compName, State: state.Name},
					Chain: chain,
				})
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Length() != out[j].Length() {
			return out[i].Length() < out[j].Length()
		}
		if out[i].Root.Component != out[j].Root.Component {
			return out[i].Root.Component < out[j].Root.Component
		}
		if out[i].Root.State != out[j].Root.State {
			return out[i].Root.State < out[j].Root.State
		}
		return chainKey(out[i].Chain) < chainKey(out[j].Chain)
	})

	for i := range out {
		out[i].ID = fmt.Sprintf("s-%04d", i+1)
	}
	return out
}

func extendChain(m *model.Model, reg *providersupport.Registry, compName string, state providersupport.StateDef, visited map[string]bool) [][]Step {
	if visited[compName] {
		return nil
	}
	visited[compName] = true
	defer delete(visited, compName)

	comp := m.Components[compName]
	if comp == nil {
		return nil
	}
	t, _, err := reg.ResolveType(effectiveProviders(comp, m), comp.Type)
	if err != nil || t == nil {
		return nil
	}

	emits := t.FailureModes[state.Name]

	// A downstream is any component whose Depends list references compName.
	downstreamSet := map[string]bool{}
	for otherName, other := range m.Components {
		for _, dep := range other.Depends {
			for _, on := range dep.On {
				if on == compName {
					downstreamSet[otherName] = true
				}
			}
		}
	}
	downstreams := make([]string, 0, len(downstreamSet))
	for k := range downstreamSet {
		downstreams = append(downstreams, k)
	}
	sort.Strings(downstreams)

	if len(downstreams) == 0 {
		if len(t.Facts) > 0 {
			step := Step{Component: compName, State: state.Name, Observes: factNames(t.Facts)}
			return [][]Step{{step}}
		}
		return nil
	}

	var allChains [][]Step
	for _, dname := range downstreams {
		dcomp := m.Components[dname]
		if dcomp == nil {
			continue
		}
		dt, _, err := reg.ResolveType(effectiveProviders(dcomp, m), dcomp.Type)
		if err != nil || dt == nil {
			continue
		}
		for _, dstate := range dt.States {
			if dstate.Name == dt.DefaultActiveState {
				continue
			}
			match := ""
			if len(dstate.TriggeredBy) == 0 {
				if len(emits) > 0 {
					match = emits[0]
				}
			} else {
				for _, l := range emits {
					for _, tb := range dstate.TriggeredBy {
						if l == tb {
							match = l
							break
						}
					}
					if match != "" {
						break
					}
				}
			}
			if match == "" {
				continue
			}

			suffixes := extendChain(m, reg, dname, dstate, visited)
			for _, suffix := range suffixes {
				full := append([]Step{{Component: compName, State: state.Name, EmitsOnEdge: match}}, suffix...)
				allChains = append(allChains, full)
			}
			if len(dt.Facts) > 0 && len(suffixes) == 0 {
				allChains = append(allChains, []Step{
					{Component: compName, State: state.Name, EmitsOnEdge: match},
					{Component: dname, State: dstate.Name, Observes: factNames(dt.Facts)},
				})
			}
		}
	}
	return allChains
}

func effectiveProviders(comp *model.Component, m *model.Model) []string {
	if len(comp.Providers) > 0 {
		return comp.Providers
	}
	return m.Meta.Providers
}

func factNames(f map[string]*providersupport.FactSpec) []string {
	out := make([]string, 0, len(f))
	for k := range f {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func chainKey(chain []Step) string {
	s := ""
	for _, step := range chain {
		s += step.Component + ":" + step.State + ">"
	}
	return s
}
