package providersupport

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// MgttVersion is the running mgtt's protocol version advertised to providers.
// Providers declare requires.mgtt as a semver range.
//
// Bump rules:
//   - patch: bug fixes that don't affect the wire protocol
//   - minor: backward-compatible additions to Command/Result/argv
//   - major: breaking protocol change
//
// Keep in sync with the VERSION file at the repo root.
const MgttVersion = "0.1.4"

// CheckCompatible returns nil if this provider is loadable for use against
// the running mgtt. Callers about to invoke the runner (probe, status,
// simulate, inspect-for-use) MUST call this. The uninstall path MUST NOT —
// you must always be able to remove a provider you can no longer use.
func (p *Provider) CheckCompatible() error {
	return CheckRequires(p.Meta.Requires)
}

// CheckRequires verifies a provider's requires constraints. Currently only
// the "mgtt" key is honored. Constraint grammar is intentionally minimal:
// only ">=X.Y.Z" is accepted. Ranges, carets, tildes are rejected at load
// time so the protocol stays predictable.
//
// A nil or empty Requires map is accepted (back-compat with pre-0.1
// providers). An unknown key is ignored (forward-compat with future deps).
func CheckRequires(requires map[string]string) error {
	for k, v := range requires {
		if k != "mgtt" {
			continue
		}
		if err := satisfiesMgtt(v); err != nil {
			return err
		}
	}
	return nil
}

func satisfiesMgtt(constraint string) error {
	constraint = strings.TrimSpace(constraint)
	if !strings.HasPrefix(constraint, ">=") {
		return fmt.Errorf("requires.mgtt %q unsupported; only \">=X.Y.Z\" is accepted", constraint)
	}
	want := normalizeSemver(strings.TrimSpace(strings.TrimPrefix(constraint, ">=")))
	have := normalizeSemver(MgttVersion)
	if !semver.IsValid(want) {
		return fmt.Errorf("requires.mgtt %q: invalid semver in constraint", constraint)
	}
	if !semver.IsValid(have) {
		return fmt.Errorf("internal: MgttVersion %q is not valid semver", MgttVersion)
	}
	if semver.Compare(have, want) < 0 {
		return fmt.Errorf("provider requires mgtt %s; running %s", constraint, MgttVersion)
	}
	return nil
}

// normalizeSemver coerces "1.0.0" → "v1.0.0" as the semver package expects.
func normalizeSemver(v string) string {
	if v == "" {
		return ""
	}
	if v[0] == 'v' {
		return v
	}
	return "v" + v
}
