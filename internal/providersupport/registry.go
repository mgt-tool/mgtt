package providersupport

import (
	"fmt"
	"strings"

	"github.com/sajonaro/mgtt/internal/expr"
)

// Registry holds all loaded providers and supports type resolution with
// pecking-order semantics.
type Registry struct {
	providers map[string]*Provider
	order     []string // insertion order for pecking-order resolution
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]*Provider),
	}
}

// Register adds a provider to the registry. Providers registered earlier
// have higher priority in pecking-order resolution.
func (r *Registry) Register(p *Provider) {
	name := p.Meta.Name
	if _, exists := r.providers[name]; !exists {
		r.order = append(r.order, name)
	}
	r.providers[name] = p
}

// Get returns a provider by name, and whether it was found.
func (r *Registry) Get(name string) (*Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// All returns all registered providers in registration order.
func (r *Registry) All() []*Provider {
	out := make([]*Provider, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.providers[name])
	}
	return out
}

// ResolveType resolves a type name to a *Type and its owning provider name.
//
// If typeName contains a dot (e.g. "aws.rds_instance"), it is treated as an
// explicit namespace and the scan is skipped — the named provider is looked up
// directly.
//
// Otherwise, the providers in componentProviders are scanned in order and the
// first one that declares typeName wins (pecking order).
func (r *Registry) ResolveType(componentProviders []string, typeName string) (*Type, string, error) {
	// Explicit namespace: "providerName.typeName"
	if dot := strings.IndexByte(typeName, '.'); dot >= 0 {
		providerName := typeName[:dot]
		localName := typeName[dot+1:]
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

	// Pecking order: scan componentProviders in order.
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

// resolveTypeInternal is a helper that looks up a type by provider+type name.
func (r *Registry) resolveTypeInternal(providerName, typeName string) (*Type, error) {
	p, ok := r.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", providerName)
	}
	t, ok := p.Types[typeName]
	if !ok {
		return nil, fmt.Errorf("type %q not found in provider %q", typeName, providerName)
	}
	return t, nil
}

// DefaultActiveStateFor returns the default_active_state for the named type.
func (r *Registry) DefaultActiveStateFor(providerName, typeName string) (string, error) {
	t, err := r.resolveTypeInternal(providerName, typeName)
	if err != nil {
		return "", err
	}
	return t.DefaultActiveState, nil
}

// FailureModesFor returns the can_cause list for a given state in a type.
func (r *Registry) FailureModesFor(providerName, typeName, stateName string) ([]string, error) {
	t, err := r.resolveTypeInternal(providerName, typeName)
	if err != nil {
		return nil, err
	}
	causes, ok := t.FailureModes[stateName]
	if !ok {
		return nil, nil // no failure modes for this state is valid
	}
	return causes, nil
}

// HealthyConditionsFor returns the compiled healthy expression nodes for a type.
func (r *Registry) HealthyConditionsFor(providerName, typeName string) ([]expr.Node, error) {
	t, err := r.resolveTypeInternal(providerName, typeName)
	if err != nil {
		return nil, err
	}
	return t.Healthy, nil
}

// FactsFor returns the facts map for a type.
func (r *Registry) FactsFor(providerName, typeName string) (map[string]*FactSpec, error) {
	t, err := r.resolveTypeInternal(providerName, typeName)
	if err != nil {
		return nil, err
	}
	return t.Facts, nil
}

// StatesFor returns the ordered state definitions for a type.
func (r *Registry) StatesFor(providerName, typeName string) ([]StateDef, error) {
	t, err := r.resolveTypeInternal(providerName, typeName)
	if err != nil {
		return nil, err
	}
	return t.States, nil
}

// ProbeCostFor returns the probe cost string for a specific fact in a type.
func (r *Registry) ProbeCostFor(providerName, typeName, factName string) (string, error) {
	t, err := r.resolveTypeInternal(providerName, typeName)
	if err != nil {
		return "", err
	}
	fs, ok := t.Facts[factName]
	if !ok {
		return "", fmt.Errorf("fact %q not found in type %q of provider %q", factName, typeName, providerName)
	}
	return fs.Probe.Cost, nil
}
