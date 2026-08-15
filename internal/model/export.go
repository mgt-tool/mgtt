// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// ExportVersion is the schema version of the document ExportJSON emits.
// Bump it when a consumer would misread an older document; a reader that
// does not recognise the version must refuse rather than guess.
const ExportVersion = 1

// The export is the RESOLVED model: provider types merged into the
// components that use them, and component-level overrides already applied.
// Two things follow from that, and both are the point.
//
// A consumer needs no registry, no install directory and no credentials —
// the boundary mgtt already defends stays defended.
//
// And mgtt's precedence rules stay on mgtt's side of the seam. A consumer
// that had to decide for itself whether a component's `healthy` replaces or
// extends its type's would be a second implementation of a rule that already
// exists here, free to drift.
type exportDoc struct {
	Version    int             `json:"mgtt_export_version"`
	Name       string          `json:"name"`
	Components []exportComp    `json:"components"`
	Types      []exportType    `json:"types"`
	Declines   []exportDecline `json:"declines"`
}

// exportDecline records something the document could not carry faithfully.
//
// The only one so far is the generic-type fallback, and it is worth a channel
// of its own rather than an error: falling back is legal in mgtt (that is what
// meta.strict_types governs), but the generic type has no facts and no states,
// so a consumer reading it would build a model of nothing and report that
// model clean. A silent pass-through would let "checked, no findings" describe
// an architecture nobody has.
type exportDecline struct {
	What string `json:"what"`
	Why  string `json:"why"`
}

type exportComp struct {
	Name         string              `json:"name"`
	Type         string              `json:"type"`
	Depends      []exportDep         `json:"depends"`
	Healthy      []string            `json:"healthy"`
	FailureModes map[string][]string `json:"failure_modes"`
}

type exportDep struct {
	On    string `json:"on"`
	While string `json:"while"`
}

type exportType struct {
	Name               string              `json:"name"`
	Provider           string              `json:"provider"`
	Facts              []exportFact        `json:"facts"`
	Healthy            []string            `json:"healthy"`
	States             []exportState       `json:"states"`
	DefaultActiveState string              `json:"default_active_state"`
	FailureModes       map[string][]string `json:"failure_modes"`
}

type exportFact struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type exportState struct {
	Name        string   `json:"name"`
	When        string   `json:"when"`
	TriggeredBy []string `json:"triggered_by"`
}

// ExportJSON renders the resolved model as the versioned JSON document.
//
// Everything with a choice of order is sorted — components, types and facts
// by name, map keys by key. Determinism is a requirement rather than
// tidiness: the document feeds a model that gets diffed across git
// revisions, and a map-iteration reshuffle would read as a change to the
// architecture itself.
//
// The one exception is a type's states, which keep their declared order.
// A consumer picking a representative assignment for a state walks that
// order, so sorting here would silently change what it picks.
func ExportJSON(m *Model, reg *providersupport.Registry) ([]byte, error) {
	doc := exportDoc{
		Version:    ExportVersion,
		Name:       m.Meta.Name,
		Components: []exportComp{},
		Types:      []exportType{},
		Declines:   []exportDecline{},
	}

	seen := map[string]bool{}
	for _, name := range m.Order {
		c, ok := m.Components[name]
		if !ok {
			continue
		}
		typ, owner, err := c.ResolveType(m, reg)
		if err != nil {
			return nil, fmt.Errorf("component %s: resolve type %q: %w", c.Name, c.Type, err)
		}
		if owner == providersupport.GenericProviderName {
			doc.Declines = append(doc.Declines, exportDecline{
				What: c.Name,
				Why: fmt.Sprintf(
					"type %q resolved to the generic fallback, which declares no facts and no states — install the provider that owns it",
					c.Type),
			})
		}
		doc.Components = append(doc.Components, exportComponent(c, typ))
		if !seen[typ.Name] {
			seen[typ.Name] = true
			doc.Types = append(doc.Types, exportProviderType(typ, owner))
		}
	}

	sort.Slice(doc.Components, func(i, j int) bool {
		return doc.Components[i].Name < doc.Components[j].Name
	})
	sort.Slice(doc.Types, func(i, j int) bool {
		return doc.Types[i].Name < doc.Types[j].Name
	})

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// exportComponent applies the override rule: a component-level healthy or
// failure_modes list replaces the type's, and an empty one inherits it.
func exportComponent(c *Component, typ *providersupport.Type) exportComp {
	out := exportComp{
		Name:    c.Name,
		Type:    c.Type,
		Depends: []exportDep{},
	}

	healthy := c.HealthyRaw
	if len(healthy) == 0 {
		healthy = typ.HealthyRaw
	}
	out.Healthy = appendCopy(healthy)

	modes := c.FailureModes
	if len(modes) == 0 {
		modes = typ.FailureModes
	}
	out.FailureModes = sortedModes(modes)

	// One depends clause may name several targets; the seam carries one edge
	// per target, since that is the granularity a consumer reasons at.
	for _, d := range c.Depends {
		for _, on := range d.On {
			out.Depends = append(out.Depends, exportDep{On: on, While: d.WhileRaw})
		}
	}
	sort.Slice(out.Depends, func(i, j int) bool {
		if out.Depends[i].On != out.Depends[j].On {
			return out.Depends[i].On < out.Depends[j].On
		}
		return out.Depends[i].While < out.Depends[j].While
	})
	return out
}

func exportProviderType(t *providersupport.Type, owner string) exportType {
	out := exportType{
		Name:               t.Name,
		Provider:           owner,
		Facts:              []exportFact{},
		Healthy:            appendCopy(t.HealthyRaw),
		States:             []exportState{},
		DefaultActiveState: t.DefaultActiveState,
		FailureModes:       sortedModes(t.FailureModes),
	}

	for name, spec := range t.Facts {
		typeName := ""
		if spec != nil {
			typeName = spec.TypeName
		}
		out.Facts = append(out.Facts, exportFact{Name: name, Type: typeName})
	}
	sort.Slice(out.Facts, func(i, j int) bool { return out.Facts[i].Name < out.Facts[j].Name })

	for _, s := range t.States {
		triggered := appendCopy(s.TriggeredBy)
		sort.Strings(triggered)
		out.States = append(out.States, exportState{
			Name:        s.Name,
			When:        s.WhenRaw,
			TriggeredBy: triggered,
		})
	}
	return out
}

// sortedModes copies in with each can_cause list sorted. A nil input becomes
// an empty map rather than JSON null, which keeps a reader's job simple.
func sortedModes(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for state, labels := range in {
		cp := appendCopy(labels)
		sort.Strings(cp)
		out[state] = cp
	}
	return out
}

// appendCopy returns a non-nil copy, so an absent list marshals as [] rather
// than null and a consumer never has to special-case the difference.
func appendCopy(in []string) []string {
	out := make([]string, 0, len(in))
	return append(out, in...)
}
