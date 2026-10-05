// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/model/build"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// ModelDiscoverParams names the model a discovery would replace, if any,
// and the components to keep although discovery no longer returns them.
type ModelDiscoverParams struct {
	ModelPath string   `json:"model_path,omitempty"`
	Tombstone []string `json:"tombstone,omitempty"`
}

// DiscoveredBy is what one provider's discovery returned.
type DiscoveredBy struct {
	Provider     string `json:"provider"`
	Components   int    `json:"components"`
	Dependencies int    `json:"dependencies"`
}

// ModelDiscoverResult is the model discovery proposes, and how it differs
// from the one it would replace. Nothing is written.
type ModelDiscoverResult struct {
	ProposedModel string            `json:"proposed_model"`
	Discovered    []DiscoveredBy    `json:"discovered"`
	Failures      map[string]string `json:"failures,omitempty"`
	Added         []string          `json:"added"`
	// Removed would need --allow-deletes or --tombstone in a real build:
	// discovered components discovery no longer returns.
	Removed      []string `json:"removed"`
	KeptAuthored []string `json:"kept_authored"`
	Dangling     []string `json:"dangling"`
}

// ModelDiscover runs every installed provider's discovery -- reading the
// live systems they cover -- and proposes the model it implies, merged
// with the existing one so authored components survive. It writes
// nothing: the proposal reaches the repository as a reviewed change.
func (h *Handler) ModelDiscover(p ModelDiscoverParams) (*ModelDiscoverResult, error) {
	var prev *model.Model
	if p.ModelPath != "" {
		m, err := model.Load(p.ModelPath)
		if err != nil {
			return nil, fmt.Errorf("model_path: %w", err)
		}
		prev = m
	}
	home, err := providersupport.Home()
	if err != nil {
		return nil, fmt.Errorf("resolve MGTT_HOME: %w", err)
	}
	plan, err := build.Discover(context.Background(), home, prev, p.Tombstone)
	if err != nil {
		return nil, err
	}
	var yaml bytes.Buffer
	if err := build.EmitYAML(plan.Next, &yaml); err != nil {
		return nil, err
	}
	out := &ModelDiscoverResult{
		ProposedModel: yaml.String(),
		Discovered:    []DiscoveredBy{},
		Added:         nonNil(plan.Diff.Added),
		Removed:       nonNil(plan.Diff.Removed),
		KeptAuthored:  nonNil(plan.Kept),
		Dangling:      nonNil(build.Dangling(plan.Next)),
	}
	for name, snap := range plan.Snapshots {
		out.Discovered = append(out.Discovered, DiscoveredBy{Provider: name, Components: len(snap.Components), Dependencies: len(snap.Dependencies)})
	}
	sort.Slice(out.Discovered, func(i, j int) bool { return out.Discovered[i].Provider < out.Discovered[j].Provider })
	if len(plan.Failures) > 0 {
		out.Failures = map[string]string{}
		for name, ferr := range plan.Failures {
			msg := ferr.Error()
			if errors.Is(ferr, providersupport.ErrNoDiscover) {
				msg = "no discovery support"
			}
			out.Failures[name] = msg
		}
	}
	return out, nil
}
