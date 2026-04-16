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
	Version   string // from provider.yaml meta.version
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
	var candidates []InstalledProvider

	for _, ip := range installed {
		if ref.LegacyBareName {
			// Bare-name: match on name only, ignore namespace.
			if !strings.EqualFold(ip.Name, ref.Name) {
				continue
			}
		} else {
			// FQN: match on both namespace and name.
			if !strings.EqualFold(ip.Name, ref.Name) {
				continue
			}
			if !strings.EqualFold(ip.Namespace, ref.Namespace) {
				continue
			}
		}

		// Check version constraint (if any).
		if ref.VersionConstraint != "" {
			if ip.Version == "" {
				// No version metadata — cannot satisfy a version constraint.
				continue
			}
			v, err := parseSemVer(ip.Version)
			if err != nil {
				// Installed version is not parseable — skip this candidate.
				continue
			}
			ok, err := v.satisfies(ref.VersionConstraint)
			if err != nil || !ok {
				continue
			}
		}

		candidates = append(candidates, ip)
	}

	if len(candidates) == 0 {
		hint := installHint(ref)
		return ResolvedProvider{}, nil, fmt.Errorf(
			"no installed provider satisfies %q (constraint: %q); install with: %s",
			refString(ref), ref.VersionConstraint, hint,
		)
	}

	// Pick the highest version among candidates.
	best := candidates[0]
	for _, c := range candidates[1:] {
		if higherVersion(c.Version, best.Version) {
			best = c
		}
	}

	rp := ResolvedProvider{
		Ref:        ref,
		Name:       best.Name,
		Version:    best.Version,
		InstallDir: best.Dir,
	}

	var warn *ResolutionWarning
	if ref.LegacyBareName {
		fqn := best.Namespace + "/" + best.Name
		if best.Namespace == "" {
			fqn = best.Name
		}
		warn = &ResolutionWarning{
			Ref: ref,
			Message: fmt.Sprintf(
				"provider ref %q is a legacy bare name; consider using the FQN form %q instead",
				ref.Name, fqn,
			),
		}
	}

	return rp, warn, nil
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
