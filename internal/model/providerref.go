// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"fmt"
	"strings"
)

// ProviderRef is a parsed provider reference from a model's `providers:` list.
// Four supported input forms:
//
//	"kubernetes"                           — legacy bare name
//	"mgt-tool/kubernetes"                  — FQN, any version
//	"kubernetes@0.5.0"                     — legacy + version
//	"mgt-tool/kubernetes@>=0.5.0,<1.0.0"  — FQN + constraint
type ProviderRef struct {
	Namespace         string // empty for legacy bare-name refs
	Name              string // required — the short provider name
	VersionConstraint string // empty = any version; may contain SemVer operators (>=, <, ^, etc.)
	LegacyBareName    bool   // true when input had no namespace; triggers validate warning
}

// ParseProviderRef parses a string from a model's `providers:` list into a
// ProviderRef. Handles all four forms.
func ParseProviderRef(s string) (ProviderRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ProviderRef{}, fmt.Errorf("provider ref must not be empty")
	}
	namePart, versionConstraint := splitVersionConstraint(s)
	namespace, name, err := splitNamespaceAndName(s, namePart)
	if err != nil {
		return ProviderRef{}, err
	}
	if err := validateRefIdentifiers(s, namespace, name); err != nil {
		return ProviderRef{}, err
	}
	return ProviderRef{
		Namespace:         namespace,
		Name:              name,
		VersionConstraint: versionConstraint,
		LegacyBareName:    namespace == "",
	}, nil
}

// splitVersionConstraint separates the name-part from an optional
// trailing @version. Only the first "@" matters — constraint bodies
// like ">=0.5.0,<1.0.0" have no extra "@".
func splitVersionConstraint(s string) (namePart, versionConstraint string) {
	if idx := strings.Index(s, "@"); idx >= 0 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

// splitNamespaceAndName splits "ns/name" into (ns, name), or returns
// ("", bare, nil) for legacy single-segment refs. More than one "/"
// is a parse error naming the original ref s.
func splitNamespaceAndName(s, namePart string) (namespace, name string, err error) {
	segments := strings.Split(namePart, "/")
	switch len(segments) {
	case 1:
		return "", segments[0], nil
	case 2:
		return segments[0], segments[1], nil
	}
	return "", "", fmt.Errorf("provider ref %q: too many path segments (expected at most namespace/name)", s)
}

// validateRefIdentifiers enforces whitespace-free / non-empty rules
// on both the namespace (when present) and the name.
func validateRefIdentifiers(s, namespace, name string) error {
	const whitespace = " \t\n\r"
	if strings.ContainsAny(namespace, whitespace) {
		return fmt.Errorf("provider ref %q: namespace must not contain whitespace", s)
	}
	if namespace == "" && strings.Contains(s, "/") {
		return fmt.Errorf("provider ref %q: namespace must not be empty", s)
	}
	if strings.ContainsAny(name, whitespace) {
		return fmt.Errorf("provider ref %q: name must not contain whitespace", s)
	}
	if name == "" {
		return fmt.Errorf("provider ref %q: name must not be empty", s)
	}
	return nil
}
