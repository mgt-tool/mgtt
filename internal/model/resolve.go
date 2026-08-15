// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"fmt"
	"strings"
)

// ResolvedProvider pairs a ProviderRef with the locally-installed provider
// that satisfies it.
type ResolvedProvider struct {
	Ref        ProviderRef
	Name       string // the short name used as the install dir key
	Version    string // the installed version that matched
	InstallDir string // filesystem path to the install
}

// ResolutionWarning is emitted (not fatal) for legacy bare-name refs.
type ResolutionWarning struct {
	Ref     ProviderRef
	Message string
}

// InstalledProvider is the minimal info the resolver needs about what's on disk.
// Callers construct this from providersupport's API.
type InstalledProvider struct {
	Name      string // short name (dir name under ~/.mgtt/providers/)
	Namespace string // from .mgtt-install.json; empty for legacy installs
	Version   string // from manifest.yaml meta.version
	Dir       string // filesystem path
}

// Resolve matches each ProviderRef against the installed set.
//
// For FQN refs (Namespace != ""):
//   - Match on Namespace + Name.
//   - If VersionConstraint is set, evaluate it against the installed Version using SemVer.
//   - If multiple match, pick the highest version.
//
// For bare-name refs (LegacyBareName = true):
//   - Match on Name only (ignore namespace).
//   - Emit a ResolutionWarning suggesting migration to FQN form.
//   - If VersionConstraint is set, still evaluate it.
//
// Returns an error listing ALL unresolved refs (not just the first), so the
// operator can fix everything in one shot. Each unresolved ref's error includes
// the install command that would fix it.
func Resolve(refs []ProviderRef, installed []InstalledProvider) ([]ResolvedProvider, []ResolutionWarning, error) {
	var resolved []ResolvedProvider
	var warnings []ResolutionWarning
	var unresolvedMsgs []string

	for _, ref := range refs {
		rp, warn, err := resolveOne(ref, installed)
		if err != nil {
			unresolvedMsgs = append(unresolvedMsgs, err.Error())
			continue
		}
		resolved = append(resolved, rp)
		if warn != nil {
			warnings = append(warnings, *warn)
		}
	}

	if len(unresolvedMsgs) > 0 {
		return resolved, warnings, fmt.Errorf(
			"provider resolution failed; %d unresolved ref(s):\n  %s",
			len(unresolvedMsgs),
			strings.Join(unresolvedMsgs, "\n  "),
		)
	}
	return resolved, warnings, nil
}

// resolveOne resolves a single ProviderRef against the installed set.
// Returns the best match, an optional warning, or an error if nothing matched.
func resolveOne(ref ProviderRef, installed []InstalledProvider) (ResolvedProvider, *ResolutionWarning, error) {
	candidates := filterMatchingInstalls(ref, installed)
	if len(candidates) == 0 {
		return ResolvedProvider{}, nil, fmt.Errorf(
			"no installed provider satisfies %q (constraint: %q); install with: %s",
			refString(ref), ref.VersionConstraint, installHint(ref),
		)
	}
	best := pickHighestVersion(candidates)
	rp := ResolvedProvider{
		Ref:        ref,
		Name:       best.Name,
		Version:    best.Version,
		InstallDir: best.Dir,
	}
	return rp, bareNameWarning(ref, best), nil
}

// filterMatchingInstalls returns every installed provider that
// satisfies ref's identity and version constraint.
func filterMatchingInstalls(ref ProviderRef, installed []InstalledProvider) []InstalledProvider {
	var out []InstalledProvider
	for _, ip := range installed {
		if !identityMatches(ref, ip) {
			continue
		}
		if !versionConstraintOK(ref.VersionConstraint, ip.Version) {
			continue
		}
		out = append(out, ip)
	}
	return out
}

// identityMatches reports whether ip matches ref's name (and namespace,
// for FQN refs). Bare-name refs match any namespace.
func identityMatches(ref ProviderRef, ip InstalledProvider) bool {
	if !strings.EqualFold(ip.Name, ref.Name) {
		return false
	}
	if ref.LegacyBareName {
		return true
	}
	return strings.EqualFold(ip.Namespace, ref.Namespace)
}

// versionConstraintOK reports whether installedVer satisfies constraint.
// An empty constraint accepts any version; an unparseable installed
// version fails every non-empty constraint.
func versionConstraintOK(constraint, installedVer string) bool {
	if constraint == "" {
		return true
	}
	if installedVer == "" {
		return false
	}
	v, err := parseSemVer(installedVer)
	if err != nil {
		return false
	}
	ok, err := v.satisfies(constraint)
	return err == nil && ok
}

// pickHighestVersion returns the candidate with the greatest version.
// Caller must pass a non-empty slice.
func pickHighestVersion(candidates []InstalledProvider) InstalledProvider {
	best := candidates[0]
	for _, c := range candidates[1:] {
		if higherVersion(c.Version, best.Version) {
			best = c
		}
	}
	return best
}

// bareNameWarning returns a migration-hint warning when ref uses the
// legacy bare-name form, suggesting the matching FQN. Returns nil for
// modern refs.
func bareNameWarning(ref ProviderRef, best InstalledProvider) *ResolutionWarning {
	if !ref.LegacyBareName {
		return nil
	}
	fqn := best.Name
	if best.Namespace != "" {
		fqn = best.Namespace + "/" + best.Name
	}
	return &ResolutionWarning{
		Ref: ref,
		Message: fmt.Sprintf(
			"provider ref %q is a legacy bare name; consider using the FQN form %q instead",
			ref.Name, fqn,
		),
	}
}

// higherVersion returns true if version string a is strictly greater than b.
// Unparseable versions are treated as lowest possible.
func higherVersion(a, b string) bool {
	va, errA := parseSemVer(a)
	vb, errB := parseSemVer(b)
	if errA != nil {
		return false // a is unparseable → not higher
	}
	if errB != nil {
		return true // b is unparseable → a is higher
	}
	return va.compare(vb) > 0
}

// refString returns a human-readable string for a ProviderRef.
func refString(ref ProviderRef) string {
	if ref.LegacyBareName {
		if ref.VersionConstraint != "" {
			return ref.Name + "@" + ref.VersionConstraint
		}
		return ref.Name
	}
	s := ref.Namespace + "/" + ref.Name
	if ref.VersionConstraint != "" {
		s += "@" + ref.VersionConstraint
	}
	return s
}

// installHint returns the suggested install command for an unresolved ref.
func installHint(ref ProviderRef) string {
	return "mgtt provider install " + refString(ref)
}
