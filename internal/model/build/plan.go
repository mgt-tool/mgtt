// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package build

import (
	"context"
	"fmt"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/sdk/provider"
)

// Plan is what a build would do: what discovery returned, the model it
// proposes after merging in what discovery cannot know, and how that
// differs from the model it would replace. Nothing is written.
type Plan struct {
	Snapshots map[string]provider.DiscoveryResult
	Failures  map[string]error // provider → why it contributed nothing
	Prev      *model.Model     // the model replaced; nil when there is none
	Next      *model.Model     // the proposed model
	Kept      []string         // authored components kept although undiscovered
	Diff      Diff
}

// Discover runs every installed provider's discovery and plans the model
// it implies, merged with prev (which may be nil). It reads live systems,
// through the providers, and writes nothing.
func Discover(ctx context.Context, mgttHome string, prev *model.Model, tombstone []string) (*Plan, error) {
	snapshots, failures, err := providersupport.DiscoverAll(ctx, mgttHome)
	if err != nil {
		return nil, fmt.Errorf("cannot read providers dir: %w", err)
	}
	next, err := BuildModel(snapshots)
	if err != nil {
		return nil, fmt.Errorf("build model: %w", err)
	}
	kept := MergePrev(prev, next, tombstone)
	return &Plan{Snapshots: snapshots, Failures: failures, Prev: prev, Next: next, Kept: kept, Diff: ComputeDiff(prev, next)}, nil
}
