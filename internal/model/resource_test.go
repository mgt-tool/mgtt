// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import "testing"

func TestResourceName(t *testing.T) {
	for _, c := range []struct {
		key, typ, resource, want string
	}{
		{"acme-shop", "deployment", "", "acme-shop"},
		{"deployment/acme-shop", "deployment", "", "acme-shop"},
		{"service/acme-shop", "service", "", "acme-shop"},
		{"service/acme-shop", "kubernetes.service", "", "acme-shop"},
		{"service/acme-shop", "service", "edge", "edge"},  // resource: wins
		{"svc/acme-shop", "service", "", "svc/acme-shop"}, // not the type: read as written
		{"/acme/app-config", "ssm_parameter", "", "/acme/app-config"},
		{"mgt-tool/mgtt", "repository", "", "mgt-tool/mgtt"},
		{"deployment/a/b", "deployment", "", "deployment/a/b"},
	} {
		comp := &Component{Name: c.key, Type: c.typ, Resource: c.resource}
		if got := comp.ResourceName(); got != c.want {
			t.Errorf("%s (type %s, resource %q): got %q, want %q", c.key, c.typ, c.resource, got, c.want)
		}
	}
}
