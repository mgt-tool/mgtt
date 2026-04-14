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

// ResolveType resolves a type name to a *Type and its owning provider.
// A typeName containing a dot ("aws.rds_instance") is an explicit namespace;
// otherwise componentProviders is scanned in order (pecking order).
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
	return nil, "", fmt.Errorf("type %q not found in any of the specified providers %v", typeName, componentProviders)
}
