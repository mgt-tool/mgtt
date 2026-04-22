package mcp

import (
	"fmt"

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
