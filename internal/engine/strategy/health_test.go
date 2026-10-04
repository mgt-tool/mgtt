// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package strategy

import (
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
)

// The verdict is three-valued: only rules that all resolve true make a
// component Healthy, and a fact nobody could read never does.
func TestComponentVerdict(t *testing.T) {
	value := func(v any) facts.Fact { return facts.Fact{Value: v, At: time.Now()} }
	status := func(s facts.FactStatus) facts.Fact { return facts.Fact{Status: s, At: time.Now()} }

	cases := []struct {
		name  string
		rules []string
		web   map[string]facts.Fact
		want  Verdict
	}{
		{"all rules true", []string{"a > 0", "b > 0"},
			map[string]facts.Fact{"a": value(1), "b": value(1)}, Healthy},
		{"one rule false", []string{"a > 0", "b > 0"},
			map[string]facts.Fact{"a": value(1), "b": value(0)}, Unhealthy},
		{"false beats unknown", []string{"a > 0", "b > 0"},
			map[string]facts.Fact{"a": value(0), "b": status(facts.FactStatusForbidden)}, Unhealthy},
		{"true and unread is unknown", []string{"a > 0", "b > 0"},
			map[string]facts.Fact{"a": value(1)}, Unknown},
		{"forbidden is unknown", []string{"a > 0"},
			map[string]facts.Fact{"a": status(facts.FactStatusForbidden)}, Unknown},
		{"transient is unknown", []string{"a > 0"},
			map[string]facts.Fact{"a": status(facts.FactStatusTransient)}, Unknown},
		{"no rules, observed", nil,
			map[string]facts.Fact{"a": value(1)}, Healthy},
		{"no rules, nothing read", nil,
			map[string]facts.Fact{"a": status(facts.FactStatusForbidden)}, Unknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, reg := tinyModel(t)
			m.Components["web"].Healthy = nil
			for _, r := range tc.rules {
				n, err := expr.Parse(r)
				if err != nil {
					t.Fatal(err)
				}
				m.Components["web"].Healthy = append(m.Components["web"].Healthy, n)
			}
			store := facts.NewInMemory()
			for k, f := range tc.web {
				f.Key = k
				store.Append("web", f)
			}
			if got := ComponentVerdict(m, reg, store, "web"); got != tc.want {
				t.Fatalf("verdict = %d, want %d", got, tc.want)
			}
		})
	}
}
