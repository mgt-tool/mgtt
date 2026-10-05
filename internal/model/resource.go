// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import "strings"

// ResourceName is the upstream resource a probe of c reads: its resource:
// when set, else its key less a prefix naming its type -- deployment/acme
// reads acme, as kubectl reads deployment/acme. The prefix lets a model key
// apart two components that share a resource name, such as a Deployment and
// the Service in front of it. Any other key reads as written: an SSM path
// or an org/repo name is not a kind.
func (c *Component) ResourceName() string {
	if c.Resource != "" {
		return c.Resource
	}
	if kind, name, ok := kindPrefixed(c.Name); ok && kind == shortType(c.Type) {
		return name
	}
	return c.Name
}

// KindKey keys a component by its type: deployment/acme. ResourceName reads
// such a key back as acme.
func KindKey(typ, name string) string { return shortType(typ) + "/" + name }

// kindPrefixed splits a key of the form kind/name, both parts non-empty and
// the name without a further slash.
func kindPrefixed(key string) (kind, name string, ok bool) {
	kind, name, ok = strings.Cut(key, "/")
	if !ok || kind == "" || name == "" || strings.Contains(name, "/") {
		return "", "", false
	}
	return kind, name, true
}

// shortType is a type name without its provider qualifier:
// kubernetes.deployment is deployment.
func shortType(t string) string {
	if i := strings.LastIndexByte(t, '.'); i >= 0 {
		return t[i+1:]
	}
	return t
}
