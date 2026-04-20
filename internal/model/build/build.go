package build

import (
	"fmt"
	"sort"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/sdk/provider"
)

// BuildModel merges per-provider DiscoveryResult snapshots into a
// single mgtt Model. Providers contribute components and
// within-provider dependencies; cross-provider wiring isn't in scope
// for this step (comes from a later catalog plan).
//
// Providers are keyed by their install name (kubernetes, aws, ...).
// Each resulting component records its originating provider in
// Component.Providers so later probe-time dispatch knows who owns it.
//
// Collision rule: if two providers return the same component name,
// BuildModel errors. Providers are expected to return stable names
// within their own domain; a collision almost always indicates a bug
// in one of them.
func BuildModel(snapshots map[string]provider.DiscoveryResult) (*model.Model, error) {
	m := &model.Model{
		Meta: model.Meta{
			Name:      "generated",
			Version:   "1.0",
			Providers: sortedKeys(snapshots),
		},
		Components: map[string]*model.Component{},
	}
	// First pass: register components.
	for _, providerName := range sortedKeys(snapshots) {
		snap := snapshots[providerName]
		for _, dc := range snap.Components {
			if existing, clash := m.Components[dc.Name]; clash {
				ownerProv := ""
				if len(existing.Providers) > 0 {
					ownerProv = existing.Providers[0]
				}
				return nil, fmt.Errorf("component name collision: %q present in providers %q and %q", dc.Name, ownerProv, providerName)
			}
			m.Components[dc.Name] = &model.Component{
				Name:      dc.Name,
				Type:      dc.Type,
				Providers: []string{providerName},
			}
		}
	}
	// Second pass: apply dependencies. Both endpoints must exist
	// within the merged model; a discovery returning an edge to an
	// unknown target is a bug to surface, not silently drop.
	for _, providerName := range sortedKeys(snapshots) {
		snap := snapshots[providerName]
		for _, dep := range snap.Dependencies {
			from, ok := m.Components[dep.From]
			if !ok {
				return nil, fmt.Errorf("provider %q: dependency from unknown component %q", providerName, dep.From)
			}
			if _, ok := m.Components[dep.To]; !ok {
				return nil, fmt.Errorf("provider %q: dependency to unknown component %q", providerName, dep.To)
			}
			from.Depends = append(from.Depends, model.Dependency{On: []string{dep.To}})
		}
	}
	return m, nil
}

func sortedKeys(m map[string]provider.DiscoveryResult) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
