package providersupport

import (
	"fmt"
	"strings"
)

// Registry holds loaded providers and resolves types with pecking-order semantics.
type Registry struct {
	providers map[string]*Provider
	order     []string // insertion order
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]*Provider)}
}

// Register adds a provider. Earlier registrations win in pecking-order resolution.
func (r *Registry) Register(p *Provider) {
	name := p.Meta.Name
	if _, exists := r.providers[name]; !exists {
		r.order = append(r.order, name)
	}
	r.providers[name] = p
}

func (r *Registry) Get(name string) (*Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

func (r *Registry) All() []*Provider {
	out := make([]*Provider, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.providers[name])
	}
	return out
}

// GenericProviderName is the meta.name of the embedded built-in "generic"
// provider. When ResolveType falls back to the generic provider, the
// returned provider name equals this constant. Duplicated from
// genericprovider.Name to avoid an import cycle (genericprovider imports
// providersupport).
const GenericProviderName = "generic"

// GenericComponentTypeName is the single fallback type the generic
// provider ships. ResolveType falls back to this type whenever a name
// lookup against componentProviders fails.
const GenericComponentTypeName = "component"

// ResolveType resolves a type name to a *Type and its owning provider.
// A typeName containing a dot ("aws.rds_instance") is an explicit namespace;
// otherwise componentProviders is scanned in order (pecking order).
//
// Fallback: when the pecking-order scan fails to find typeName and the
// embedded "generic" provider is registered, the lookup returns
// generic.component. This lets models declaring unknown types (or omitting
// the provider list entirely) resolve to an operator-verified component
// rather than failing parse. Callers that want strict resolution should
// build a registry without calling genericprovider.Register.
func (r *Registry) ResolveType(componentProviders []string, typeName string) (*Type, string, error) {
	if dot := strings.IndexByte(typeName, '.'); dot >= 0 {
		providerName, localName := typeName[:dot], typeName[dot+1:]
		p, ok := r.providers[providerName]
		if !ok {
			return nil, "", fmt.Errorf("provider %q not found", providerName)
		}
		t, ok := p.Types[localName]
		if !ok {
			return nil, "", fmt.Errorf("type %q not found in provider %q", localName, providerName)
		}
		return t, providerName, nil
	}

	for _, providerName := range componentProviders {
		p, ok := r.providers[providerName]
		if !ok {
			continue
		}
		if t, ok := p.Types[typeName]; ok {
			return t, providerName, nil
		}
	}

	// Fallback to generic.component when the embedded generic provider is
	// registered. Only reached when the normal pecking-order scan fails.
	if gen, ok := r.providers[GenericProviderName]; ok {
		if t, ok := gen.Types[GenericComponentTypeName]; ok {
			return t, GenericProviderName, nil
		}
	}
	return nil, "", fmt.Errorf("type %q not found in any of the specified providers %v", typeName, componentProviders)
}
