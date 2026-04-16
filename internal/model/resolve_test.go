package model

import (
	"strings"
	"testing"
)

// helper to build a simple FQN ref without touching ParseProviderRef.
func fqnRef(ns, name, constraint string) ProviderRef {
	return ProviderRef{Namespace: ns, Name: name, VersionConstraint: constraint}
}

func bareRef(name, constraint string) ProviderRef {
	return ProviderRef{Name: name, VersionConstraint: constraint, LegacyBareName: true}
}

func ip(name, ns, version, dir string) InstalledProvider {
	return InstalledProvider{Name: name, Namespace: ns, Version: version, Dir: dir}
}

// TestResolve_ExactMatch: FQN ref with exact version, one matching installed provider.
func TestResolve_ExactMatch(t *testing.T) {
	refs := []ProviderRef{fqnRef("mgt-tool", "kubernetes", "0.5.0")}
	installed := []InstalledProvider{
		ip("kubernetes", "mgt-tool", "0.5.0", "/providers/kubernetes"),
	}

	resolved, warnings, err := Resolve(refs, installed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %d", len(warnings))
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved, got %d", len(resolved))
	}
	r := resolved[0]
	if r.Name != "kubernetes" {
		t.Errorf("Name: got %q, want %q", r.Name, "kubernetes")
	}
	if r.Version != "0.5.0" {
		t.Errorf("Version: got %q, want %q", r.Version, "0.5.0")
	}
	if r.InstallDir != "/providers/kubernetes" {
		t.Errorf("InstallDir: got %q, want %q", r.InstallDir, "/providers/kubernetes")
	}
}

// TestResolve_RangeMatch: FQN ref with range, installed version within range.
func TestResolve_RangeMatch(t *testing.T) {
	refs := []ProviderRef{fqnRef("mgt-tool", "tempo", ">=0.5.0,<1.0.0")}
	installed := []InstalledProvider{
		ip("tempo", "mgt-tool", "0.7.3", "/providers/tempo"),
	}

	resolved, _, err := Resolve(refs, installed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Version != "0.7.3" {
		t.Errorf("expected resolved version 0.7.3, got %v", resolved)
	}
}

// TestResolve_PicksHighest: two installed providers with same FQN, different
// versions; ref range includes both → resolver picks higher version.
func TestResolve_PicksHighest(t *testing.T) {
	refs := []ProviderRef{fqnRef("mgt-tool", "kubernetes", ">=0.5.0,<1.0.0")}
	installed := []InstalledProvider{
		ip("kubernetes", "mgt-tool", "0.5.0", "/providers/kubernetes-0.5.0"),
		ip("kubernetes", "mgt-tool", "0.8.0", "/providers/kubernetes-0.8.0"),
	}

	resolved, _, err := Resolve(refs, installed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved, got %d", len(resolved))
	}
	if resolved[0].Version != "0.8.0" {
		t.Errorf("expected highest version 0.8.0, got %q", resolved[0].Version)
	}
	if resolved[0].InstallDir != "/providers/kubernetes-0.8.0" {
		t.Errorf("expected dir for 0.8.0, got %q", resolved[0].InstallDir)
	}
}

// TestResolve_NoMatch_ErrorIncludesInstallHint: ref asks for a version nobody
// has → error mentions "mgtt provider install <ref>".
func TestResolve_NoMatch_ErrorIncludesInstallHint(t *testing.T) {
	refs := []ProviderRef{fqnRef("mgt-tool", "aws", ">=2.0.0")}
	installed := []InstalledProvider{
		ip("aws", "mgt-tool", "1.0.0", "/providers/aws"),
	}

	_, _, err := Resolve(refs, installed)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "mgtt provider install") {
		t.Errorf("error missing install hint: %q", errStr)
	}
	if !strings.Contains(errStr, "mgt-tool/aws") {
		t.Errorf("error missing provider name: %q", errStr)
	}
}

// TestResolve_BareName_WarnsAndMatches: legacy bare ref → matches on Name,
// emits ResolutionWarning.
func TestResolve_BareName_WarnsAndMatches(t *testing.T) {
	refs := []ProviderRef{bareRef("kubernetes", "")}
	installed := []InstalledProvider{
		ip("kubernetes", "mgt-tool", "0.5.0", "/providers/kubernetes"),
	}

	resolved, warnings, err := Resolve(refs, installed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved, got %d", len(resolved))
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if !strings.Contains(warnings[0].Message, "legacy bare name") {
		t.Errorf("warning missing 'legacy bare name': %q", warnings[0].Message)
	}
}

// TestResolve_BareName_WithVersion: bare ref with version constraint → still matches.
func TestResolve_BareName_WithVersion(t *testing.T) {
	refs := []ProviderRef{bareRef("tempo", ">=0.2.0")}
	installed := []InstalledProvider{
		ip("tempo", "mgt-tool", "0.3.0", "/providers/tempo"),
	}

	resolved, warnings, err := Resolve(refs, installed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved, got %d", len(resolved))
	}
	if resolved[0].Version != "0.3.0" {
		t.Errorf("expected version 0.3.0, got %q", resolved[0].Version)
	}
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning (bare name), got %d", len(warnings))
	}
}

// TestResolve_MultipleUnresolved_ErrorListsAll: two refs, both unresolved →
// error contains both install hints.
func TestResolve_MultipleUnresolved_ErrorListsAll(t *testing.T) {
	refs := []ProviderRef{
		fqnRef("mgt-tool", "aws", ">=1.0.0"),
		fqnRef("mgt-tool", "kubernetes", ">=2.0.0"),
	}
	installed := []InstalledProvider{
		ip("aws", "mgt-tool", "0.5.0", "/providers/aws"),
		ip("kubernetes", "mgt-tool", "1.0.0", "/providers/kubernetes"),
	}

	_, _, err := Resolve(refs, installed)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "2 unresolved ref(s)") {
		t.Errorf("error missing unresolved count: %q", errStr)
	}
	if !strings.Contains(errStr, "mgt-tool/aws") {
		t.Errorf("error missing aws ref: %q", errStr)
	}
	if !strings.Contains(errStr, "mgt-tool/kubernetes") {
		t.Errorf("error missing kubernetes ref: %q", errStr)
	}
}

// TestResolve_CaretConstraint: ^0.2 should match 0.2.0, 0.2.5, not 0.3.0.
func TestResolve_CaretConstraint(t *testing.T) {
	ref := fqnRef("mgt-tool", "tempo", "^0.2")

	// Should match 0.2.0
	resolved, _, err := Resolve([]ProviderRef{ref}, []InstalledProvider{
		ip("tempo", "mgt-tool", "0.2.0", "/providers/tempo"),
	})
	if err != nil {
		t.Fatalf("0.2.0 should satisfy ^0.2: %v", err)
	}
	if len(resolved) != 1 {
		t.Errorf("expected 1 resolved for 0.2.0, got %d", len(resolved))
	}

	// Should match 0.2.5
	resolved, _, err = Resolve([]ProviderRef{ref}, []InstalledProvider{
		ip("tempo", "mgt-tool", "0.2.5", "/providers/tempo"),
	})
	if err != nil {
		t.Fatalf("0.2.5 should satisfy ^0.2: %v", err)
	}
	if len(resolved) != 1 {
		t.Errorf("expected 1 resolved for 0.2.5, got %d", len(resolved))
	}

	// Should NOT match 0.3.0
	_, _, err = Resolve([]ProviderRef{ref}, []InstalledProvider{
		ip("tempo", "mgt-tool", "0.3.0", "/providers/tempo"),
	})
	if err == nil {
		t.Error("0.3.0 should NOT satisfy ^0.2, expected error")
	}
}

// TestResolve_EmptyConstraint_MatchesAny: no version constraint → any installed
// version works.
func TestResolve_EmptyConstraint_MatchesAny(t *testing.T) {
	refs := []ProviderRef{fqnRef("mgt-tool", "docker", "")}
	installed := []InstalledProvider{
		ip("docker", "mgt-tool", "99.0.0", "/providers/docker"),
	}

	resolved, _, err := Resolve(refs, installed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Version != "99.0.0" {
		t.Errorf("expected to resolve docker at 99.0.0, got %v", resolved)
	}
}

// TestResolve_NamespaceMismatch: FQN ref does NOT match a provider in a
// different namespace, even if the name matches.
func TestResolve_NamespaceMismatch(t *testing.T) {
	refs := []ProviderRef{fqnRef("mgt-tool", "kubernetes", "")}
	installed := []InstalledProvider{
		ip("kubernetes", "other-ns", "1.0.0", "/providers/kubernetes"),
	}

	_, _, err := Resolve(refs, installed)
	if err == nil {
		t.Fatal("expected error for namespace mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "mgtt provider install") {
		t.Errorf("error missing install hint: %q", err.Error())
	}
}

// TestResolve_EmptyInstalled: no providers at all → all refs fail.
func TestResolve_EmptyInstalled(t *testing.T) {
	refs := []ProviderRef{
		fqnRef("mgt-tool", "aws", ""),
		fqnRef("mgt-tool", "kubernetes", ""),
	}

	_, _, err := Resolve(refs, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "2 unresolved ref(s)") {
		t.Errorf("unexpected error: %q", err.Error())
	}
}
