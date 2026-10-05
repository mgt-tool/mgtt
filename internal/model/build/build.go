// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package build

import (
	"fmt"
	"maps"
	"slices"

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
// Collision rule: a name two providers both return -- a Deployment and an
// RDS instance both called api -- is keyed by kind in each,
// deployment/api and rds_instance/api, which probes read back as api. A
// name one provider returns twice is an error: its dependencies could not
// say which one they mean. So is a kind key that still collides.
func BuildModel(snapshots map[string]provider.DiscoveryResult) (*model.Model, error) {
	m := &model.Model{
		Meta: model.Meta{
			Name:      "generated",
			Version:   "1.0",
			Providers: slices.Sorted(maps.Keys(snapshots)),
		},
		Components: map[string]*model.Component{},
	}
	providers := slices.Sorted(maps.Keys(snapshots))
	returnedBy := map[string]int{} // name -> how many providers return it
	for _, providerName := range providers {
		seen := map[string]bool{}
		for _, dc := range snapshots[providerName].Components {
			if seen[dc.Name] {
				return nil, fmt.Errorf("provider %q returned component %q twice", providerName, dc.Name)
			}
			seen[dc.Name] = true
			returnedBy[dc.Name]++
		}
	}
	// First pass: register components, remembering each provider's key
	// for every name it returned.
	keyOf := map[string]map[string]string{}
	for _, providerName := range providers {
		keyOf[providerName] = map[string]string{}
		for _, dc := range snapshots[providerName].Components {
			key := dc.Name
			if returnedBy[dc.Name] > 1 {
				key = model.KindKey(dc.Type, dc.Name)
			}
			if existing, clash := m.Components[key]; clash {
				return nil, fmt.Errorf("component name collision: %q present in providers %q and %q", key, existing.Providers[0], providerName)
			}
			keyOf[providerName][dc.Name] = key
			m.Components[key] = &model.Component{
				Name:      key,
				Type:      dc.Type,
				Providers: []string{providerName},
				Source:    model.SourceDiscovered,
			}
		}
	}
	// Second pass: apply dependencies. Both endpoints must exist among the
	// provider's own components; a discovery returning an edge to an
	// unknown target is a bug to surface, not silently drop.
	for _, providerName := range providers {
		for _, dep := range snapshots[providerName].Dependencies {
			from, ok := keyOf[providerName][dep.From]
			if !ok {
				return nil, fmt.Errorf("provider %q: dependency from unknown component %q", providerName, dep.From)
			}
			to, ok := keyOf[providerName][dep.To]
			if !ok {
				return nil, fmt.Errorf("provider %q: dependency to unknown component %q", providerName, dep.To)
			}
			c := m.Components[from]
			c.Depends = append(c.Depends, model.Dependency{On: []string{to}})
		}
	}
	return m, nil
}
