// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"fmt"

	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/genericprovider"
)

// loadContext loads a model from the given path and builds a provider
// registry ready for engine.Plan / state.Derive. Mirrors what the CLI
// assembles in registry.go but scoped to a single call — no globals.
//
// The registry includes every provider installed under $MGTT_HOME plus
// the embedded generic fallback.
func loadContext(modelRef string) (*model.Model, *providersupport.Registry, error) {
	m, err := model.Load(modelRef)
	if err != nil {
		return nil, nil, fmt.Errorf("load model %q: %w", modelRef, err)
	}
	reg, reserved := providersupport.LoadAllForUse()
	if len(reserved) > 0 {
		return nil, nil, fmt.Errorf("installed provider(s) %v claim the reserved name %q", reserved, providersupport.GenericProviderName)
	}
	if err := genericprovider.Register(reg); err != nil {
		return nil, nil, fmt.Errorf("register generic fallback: %w", err)
	}
	return m, reg, nil
}

// withIncident runs fn inside the per-incident lock with the loaded
// Incident. writer=true holds the write lock (FactAdd, Probe execute path);
// false uses the read lock (FactsList, Plan, ScenariosList, snapshot).
// Centralises the 8-line prologue every tool method shared.
func withIncident[R any](id string, writer bool, fn func(*incident.Incident) (R, error)) (R, error) {
	var zero R
	if id == "" {
		return zero, fmt.Errorf("incident_id is required")
	}
	mu := lockFor(id)
	if writer {
		mu.Lock()
		defer mu.Unlock()
	} else {
		mu.RLock()
		defer mu.RUnlock()
	}
	inc, err := incident.LoadByID(id)
	if err != nil {
		return zero, fmt.Errorf("load incident: %w", err)
	}
	return fn(inc)
}

// withModelContext extends withIncident by enforcing model_ref and loading
// the model + registry. Used by plan, probe, scenarios, snapshot — every
// tool that needs engine state, not just the fact store.
func withModelContext[R any](id string, writer bool, fn func(*incident.Incident, *model.Model, *providersupport.Registry) (R, error)) (R, error) {
	return withIncident(id, writer, func(inc *incident.Incident) (R, error) {
		var zero R
		if inc.ModelRef == "" {
			return zero, fmt.Errorf("incident %q has no model_ref — was it started via MCP?", id)
		}
		m, reg, err := loadContext(inc.ModelRef)
		if err != nil {
			return zero, err
		}
		return fn(inc, m, reg)
	})
}
