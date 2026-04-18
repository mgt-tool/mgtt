package genericprovider

import (
	"testing"

	"github.com/mgt-tool/mgtt/internal/providersupport"
)

func TestRegister_LoadsComponentType(t *testing.T) {
	reg := providersupport.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Resolve generic.component via the explicit-namespace form, which does
	// not require the generic provider to appear in componentProviders.
	t1, prov, err := reg.ResolveType(nil, "generic.component")
	if err != nil {
		t.Fatalf("ResolveType generic.component: %v", err)
	}
	if prov != Name {
		t.Errorf("provider = %q; want %q", prov, Name)
	}
	if t1.Name != "component" {
		t.Errorf("type.Name = %q; want component", t1.Name)
	}
	if len(t1.States) < 2 {
		t.Errorf("want at least 2 states (live, stopped); got %d", len(t1.States))
	}
	if t1.DefaultActiveState != "live" {
		t.Errorf("default_active_state = %q; want live", t1.DefaultActiveState)
	}

	// Meta / provider shape sanity.
	p, ok := reg.Get(Name)
	if !ok {
		t.Fatalf("Registry.Get(%q) = false", Name)
	}
	if p.Meta.Version == "" {
		t.Error("meta.version is empty")
	}
}

func TestResolveType_FallsBackToGenericComponent(t *testing.T) {
	reg := providersupport.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Unknown type + unknown provider — fallback MUST kick in.
	t1, prov, err := reg.ResolveType([]string{"nonexistent-provider"}, "whatever-type")
	if err != nil {
		t.Fatalf("ResolveType (fallback): %v", err)
	}
	if prov != Name {
		t.Errorf("want fallback to %q; got provider=%q", Name, prov)
	}
	if t1.Name != "component" {
		t.Errorf("want type=component; got %q", t1.Name)
	}
}

func TestResolveType_NoFallbackWithoutRegister(t *testing.T) {
	// Without Register, unknown types still produce an error — the
	// fallback is explicitly opt-in.
	reg := providersupport.NewRegistry()
	if _, _, err := reg.ResolveType([]string{"x"}, "whatever"); err == nil {
		t.Fatal("ResolveType on empty registry should fail; fallback must be opt-in")
	}
}
