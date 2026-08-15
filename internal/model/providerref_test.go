// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import "testing"

func TestParseProviderRef(t *testing.T) {
	cases := []struct {
		input   string
		want    ProviderRef
		wantErr bool
	}{
		// Bare name (legacy)
		{"kubernetes", ProviderRef{Name: "kubernetes", LegacyBareName: true}, false},
		// FQN without version
		{"mgt-tool/kubernetes", ProviderRef{Namespace: "mgt-tool", Name: "kubernetes"}, false},
		// Bare name + version
		{"kubernetes@0.5.0", ProviderRef{Name: "kubernetes", VersionConstraint: "0.5.0", LegacyBareName: true}, false},
		// FQN + exact version
		{"mgt-tool/tempo@0.2.0", ProviderRef{Namespace: "mgt-tool", Name: "tempo", VersionConstraint: "0.2.0"}, false},
		// FQN + version range
		{"mgt-tool/kubernetes@>=0.5.0,<1.0.0", ProviderRef{Namespace: "mgt-tool", Name: "kubernetes", VersionConstraint: ">=0.5.0,<1.0.0"}, false},
		// FQN + caret constraint
		{"mgt-tool/aws@^0.2", ProviderRef{Namespace: "mgt-tool", Name: "aws", VersionConstraint: "^0.2"}, false},
		// Edge: whitespace trimming
		{"  mgt-tool/tempo@0.2.0  ", ProviderRef{Namespace: "mgt-tool", Name: "tempo", VersionConstraint: "0.2.0"}, false},
		// Errors
		{"", ProviderRef{}, true},      // empty
		{"a/b/c", ProviderRef{}, true}, // too many slashes
		{"/name", ProviderRef{}, true}, // empty namespace
		{"ns/", ProviderRef{}, true},   // empty name
	}
	for _, tc := range cases {
		got, err := ParseProviderRef(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseProviderRef(%q): err=%v, wantErr=%v", tc.input, err, tc.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if got != tc.want {
			t.Errorf("ParseProviderRef(%q):\n got  %+v\n want %+v", tc.input, got, tc.want)
		}
	}
}
