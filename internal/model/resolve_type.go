// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import "github.com/mgt-tool/mgtt/internal/providersupport"

// ResolveType looks up the component's type against the registry, using
// the component's effective providers list. The returned owner is the
// provider short name that satisfied the lookup. Callers that previously
// re-implemented the "pick component.Providers else meta.Providers,
// then reg.ResolveType" dance should use this instead.
func (c *Component) ResolveType(m *Model, reg *providersupport.Registry) (*providersupport.Type, string, error) {
	return reg.ResolveType(c.EffectiveProviders(m), c.Type)
}
