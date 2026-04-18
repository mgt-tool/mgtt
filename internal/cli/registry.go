package cli

import (
	"fmt"

	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/genericprovider"
)

// loadRegistryForUse loads every discovered provider via LoadAllForUse and
// registers the embedded generic fallback provider. Callers that want
// strict type-resolution (no fallback) should use providersupport
// directly — this helper is the "normal" path used by plan / diagnose /
// simulate / status / model validate.
func loadRegistryForUse() (*providersupport.Registry, error) {
	reg := providersupport.LoadAllForUse()
	if err := genericprovider.Register(reg); err != nil {
		return nil, fmt.Errorf("register generic provider: %w", err)
	}
	return reg, nil
}

// loadRegistryAll mirrors loadRegistryForUse but uses LoadAllEmbedded
// instead (no compatibility filter). Used by `mgtt ls` and similar
// discovery paths that must still see incompatible providers.
func loadRegistryAll() (*providersupport.Registry, error) {
	reg := providersupport.LoadAllEmbedded()
	if err := genericprovider.Register(reg); err != nil {
		return nil, fmt.Errorf("register generic provider: %w", err)
	}
	return reg, nil
}
