package model

// Integration tests for the provider resolution flow, exercising ParseProviderRef →
// Resolve end-to-end with realistic inputs matching the four forms documented in
// docs/concepts/provider-fqn-and-versions.md.
//
// These do not require a running daemon, a real provider install, or building the
// mgtt binary.  They import the model package directly and call Resolve.

import (
	"strings"
	"testing"
)

// fixtureInstalled represents a provider staged as though installed by
// `mgtt provider install`, with namespace derived from the git URL.
var fixtureInstalled = InstalledProvider{
	Name:      "fixture",
	Namespace: "test-org",
	Version:   "0.1.0",
	Dir:       "/tmp/fake-mgtt-home/providers/fixture",
}

// TestResolveIntegration_FQNExactPin verifies that a model referencing
// "test-org/fixture@0.1.0" resolves successfully against the fixture provider.
func TestResolveIntegration_FQNExactPin(t *testing.T) {
	ref, err := ParseProviderRef("test-org/fixture@0.1.0")
	if err != nil {
		t.Fatalf("ParseProviderRef: %v", err)
	}

	resolved, warnings, err := Resolve([]ProviderRef{ref}, []InstalledProvider{fixtureInstalled})
	if err != nil {
		t.Fatalf("unexpected resolution error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %d: %v", len(warnings), warnings)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved provider, got %d", len(resolved))
	}
	r := resolved[0]
	if r.Name != "fixture" {
		t.Errorf("Name: want %q, got %q", "fixture", r.Name)
	}
	if r.Version != "0.1.0" {
		t.Errorf("Version: want %q, got %q", "0.1.0", r.Version)
	}
}

// TestResolveIntegration_FQNImpossibleConstraint verifies that a model
// referencing "test-org/fixture@>=99.0.0" fails with an install-hint error.
// This is the "unresolvable" case from the docs: resolution fails before any
// probe attempt.
func TestResolveIntegration_FQNImpossibleConstraint(t *testing.T) {
	ref, err := ParseProviderRef("test-org/fixture@>=99.0.0")
	if err != nil {
		t.Fatalf("ParseProviderRef: %v", err)
	}

	_, _, err = Resolve([]ProviderRef{ref}, []InstalledProvider{fixtureInstalled})
	if err == nil {
		t.Fatal("expected resolution error for impossible constraint, got nil")
	}

	errStr := err.Error()

	// Error must name the provider so the operator knows what to fix.
	if !strings.Contains(errStr, "test-org/fixture") {
		t.Errorf("error missing provider name %q:\n%s", "test-org/fixture", errStr)
	}

	// Error must include the install hint so the operator knows the fix.
	if !strings.Contains(errStr, "mgtt provider install") {
		t.Errorf("error missing install hint:\n%s", errStr)
	}

	// Error must NOT be confused with a binary-not-found error; it must clearly
	// say resolution failed, not that the binary was missing.
	if !strings.Contains(errStr, "unresolved ref") {
		t.Errorf("error missing 'unresolved ref' language (would be confused with probe failure):\n%s", errStr)
	}
}

// TestResolveIntegration_BareNameWarns verifies that a model referencing "fixture"
// (no namespace, no version) resolves successfully but emits a deprecation warning
// suggesting the FQN form.
func TestResolveIntegration_BareNameWarns(t *testing.T) {
	ref, err := ParseProviderRef("fixture")
	if err != nil {
		t.Fatalf("ParseProviderRef: %v", err)
	}
	if !ref.LegacyBareName {
		t.Fatal("expected LegacyBareName=true for bare-name ref")
	}

	resolved, warnings, err := Resolve([]ProviderRef{ref}, []InstalledProvider{fixtureInstalled})
	if err != nil {
		t.Fatalf("unexpected resolution error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved provider, got %d", len(resolved))
	}

	// Must emit exactly one warning about the bare name.
	if len(warnings) != 1 {
		t.Fatalf("expected 1 deprecation warning, got %d", len(warnings))
	}
	if !strings.Contains(warnings[0].Message, "legacy bare name") {
		t.Errorf("warning missing 'legacy bare name': %q", warnings[0].Message)
	}

	// Warning must suggest the FQN form so the operator knows what to use instead.
	if !strings.Contains(warnings[0].Message, "test-org/fixture") {
		t.Errorf("warning missing suggested FQN %q: %q", "test-org/fixture", warnings[0].Message)
	}
}

// TestResolveIntegration_FQNRange verifies the range form from the docs:
// "mgt-tool/kubernetes@>=0.5.0,<1.0.0" resolves against an installed 0.7.3.
func TestResolveIntegration_FQNRange(t *testing.T) {
	ref, err := ParseProviderRef("mgt-tool/kubernetes@>=0.5.0,<1.0.0")
	if err != nil {
		t.Fatalf("ParseProviderRef: %v", err)
	}

	installed := InstalledProvider{
		Name:      "kubernetes",
		Namespace: "mgt-tool",
		Version:   "0.7.3",
		Dir:       "/fake/providers/kubernetes",
	}

	resolved, _, err := Resolve([]ProviderRef{ref}, []InstalledProvider{installed})
	if err != nil {
		t.Fatalf("unexpected resolution error: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Version != "0.7.3" {
		t.Errorf("expected resolution to 0.7.3, got %v", resolved)
	}
}

// TestResolveIntegration_CaretForm verifies the caret form from the docs:
// "mgt-tool/aws@^0.2" resolves against 0.2.5 but not 0.3.0.
func TestResolveIntegration_CaretForm(t *testing.T) {
	ref, err := ParseProviderRef("mgt-tool/aws@^0.2")
	if err != nil {
		t.Fatalf("ParseProviderRef: %v", err)
	}

	matchingInstall := InstalledProvider{
		Name:      "aws",
		Namespace: "mgt-tool",
		Version:   "0.2.5",
		Dir:       "/fake/providers/aws",
	}
	nonMatchingInstall := InstalledProvider{
		Name:      "aws",
		Namespace: "mgt-tool",
		Version:   "0.3.0",
		Dir:       "/fake/providers/aws",
	}

	// 0.2.5 satisfies ^0.2 (>=0.2.0,<0.3.0).
	resolved, _, err := Resolve([]ProviderRef{ref}, []InstalledProvider{matchingInstall})
	if err != nil {
		t.Fatalf("0.2.5 should satisfy ^0.2: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Version != "0.2.5" {
		t.Errorf("expected resolution to 0.2.5, got %v", resolved)
	}

	// 0.3.0 does NOT satisfy ^0.2.
	_, _, err = Resolve([]ProviderRef{ref}, []InstalledProvider{nonMatchingInstall})
	if err == nil {
		t.Error("0.3.0 should NOT satisfy ^0.2, expected resolution error")
	}
}

// TestResolveIntegration_AllFourForms exercises all four documented forms in a
// single Resolve call to confirm they interact correctly when mixed in one model.
func TestResolveIntegration_AllFourForms(t *testing.T) {
	rawRefs := []string{
		"mgt-tool/kubernetes@>=0.5.0,<1.0.0",
		"mgt-tool/tempo@0.2.0",
		"mgt-tool/aws@^0.2",
		"kubernetes", // bare name — should warn
	}

	var refs []ProviderRef
	for _, s := range rawRefs {
		r, err := ParseProviderRef(s)
		if err != nil {
			t.Fatalf("ParseProviderRef(%q): %v", s, err)
		}
		refs = append(refs, r)
	}

	installed := []InstalledProvider{
		{Name: "kubernetes", Namespace: "mgt-tool", Version: "0.7.0", Dir: "/fake/providers/kubernetes"},
		{Name: "tempo", Namespace: "mgt-tool", Version: "0.2.0", Dir: "/fake/providers/tempo"},
		{Name: "aws", Namespace: "mgt-tool", Version: "0.2.3", Dir: "/fake/providers/aws"},
	}

	resolved, warnings, err := Resolve(refs, installed)
	if err != nil {
		t.Fatalf("unexpected resolution error: %v", err)
	}

	// Three FQN refs plus one bare-name ref → 4 resolved.
	if len(resolved) != 4 {
		t.Errorf("expected 4 resolved, got %d", len(resolved))
	}

	// Bare-name "kubernetes" should emit exactly one warning.
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning (bare name), got %d", len(warnings))
	}
	if !strings.Contains(warnings[0].Message, "legacy bare name") {
		t.Errorf("unexpected warning content: %q", warnings[0].Message)
	}
}
