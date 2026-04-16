package model

import (
	"fmt"
	"strconv"
	"strings"
)

// semVersion is a simple Major.Minor.Patch representation.
// Pre-release and build metadata are intentionally ignored.
type semVersion struct {
	Major, Minor, Patch int
}

// parseSemVer parses "1.2.3" or "v1.2.3" into semVersion.
// A leading "v" is stripped before parsing.
// Returns an error for unparseable strings.
func parseSemVer(s string) (semVersion, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")

	// Strip pre-release / build metadata suffixes: everything after the first
	// "-" or "+" that follows the patch integer.
	if idx := strings.IndexAny(s, "-+"); idx >= 0 {
		s = s[:idx]
	}

	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return semVersion{}, fmt.Errorf("semver: cannot parse %q", s)
	}

	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semVersion{}, fmt.Errorf("semver: cannot parse %q: segment %q is not a non-negative integer", s, p)
		}
		nums[i] = n
	}
	return semVersion{Major: nums[0], Minor: nums[1], Patch: nums[2]}, nil
}

// compare returns -1 if a < b, 0 if a == b, 1 if a > b.
func (a semVersion) compare(b semVersion) int {
	switch {
	case a.Major != b.Major:
		if a.Major < b.Major {
			return -1
		}
		return 1
	case a.Minor != b.Minor:
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	case a.Patch != b.Patch:
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	default:
		return 0
	}
}

// satisfies returns true if v satisfies the constraint string.
//
// Supported forms (may be comma-separated to AND them):
//   - ""         — any version matches
//   - "1.2.3"   — exact match
//   - ">=1.2.3" — greater-or-equal
//   - ">1.2.3"  — strictly greater
//   - "<=1.2.3" — less-or-equal
//   - "<1.2.3"  — strictly less
//   - "^0.2"    — >=0.2.0,<0.3.0  (next minor boundary)
//   - "^1.2"    — >=1.2.0,<2.0.0  (next major boundary when major >= 1)
func (v semVersion) satisfies(constraint string) (bool, error) {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return true, nil
	}

	// Expand caret before splitting on commas.
	expanded, err := expandCaret(constraint)
	if err != nil {
		return false, err
	}

	parts := strings.Split(expanded, ",")
	for _, part := range parts {
		ok, err := v.satisfiesSingle(strings.TrimSpace(part))
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// expandCaret replaces any ^X.Y[.Z] token in the constraint string with
// the equivalent >=X.Y.Z,<next range. Multiple caret tokens may appear
// if separated by commas, though that is uncommon.
func expandCaret(constraint string) (string, error) {
	parts := strings.Split(constraint, ",")
	out := make([]string, 0, len(parts)*2)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if !strings.HasPrefix(p, "^") {
			out = append(out, p)
			continue
		}
		vstr := strings.TrimPrefix(p, "^")
		base, err := parseSemVer(vstr)
		if err != nil {
			return "", fmt.Errorf("semver: cannot expand caret constraint %q: %w", p, err)
		}
		lo := fmt.Sprintf("%d.%d.%d", base.Major, base.Minor, base.Patch)
		var hi string
		if base.Major >= 1 {
			// ^1.2 → >=1.2.0,<2.0.0
			hi = fmt.Sprintf("%d.0.0", base.Major+1)
		} else {
			// ^0.2 → >=0.2.0,<0.3.0
			hi = fmt.Sprintf("0.%d.0", base.Minor+1)
		}
		out = append(out, ">="+lo, "<"+hi)
	}
	return strings.Join(out, ","), nil
}

// satisfiesSingle evaluates a single (non-comma) constraint token.
func (v semVersion) satisfiesSingle(c string) (bool, error) {
	var op, vstr string
	switch {
	case strings.HasPrefix(c, ">="):
		op, vstr = ">=", c[2:]
	case strings.HasPrefix(c, "<="):
		op, vstr = "<=", c[2:]
	case strings.HasPrefix(c, ">"):
		op, vstr = ">", c[1:]
	case strings.HasPrefix(c, "<"):
		op, vstr = "<", c[1:]
	default:
		op, vstr = "=", c // bare version → exact match
	}

	other, err := parseSemVer(vstr)
	if err != nil {
		return false, fmt.Errorf("semver: cannot parse version in constraint %q: %w", c, err)
	}

	cmp := v.compare(other)
	switch op {
	case "=":
		return cmp == 0, nil
	case ">=":
		return cmp >= 0, nil
	case ">":
		return cmp > 0, nil
	case "<=":
		return cmp <= 0, nil
	case "<":
		return cmp < 0, nil
	default:
		return false, fmt.Errorf("semver: unknown operator %q", op)
	}
}
