// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"fmt"
	"strings"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// Validate runs all validation passes against the loaded model and returns a
// ValidationResult. When reg is non-nil, pass 2 resolves component types
// against the provider registry.
func Validate(m *Model, reg *providersupport.Registry) *ValidationResult {
	result := &ValidationResult{}

	pass1Structural(m, result)
	if reg != nil {
		pass2TypeResolution(m, reg, result)
		pass5TriggeredBy(m, reg, result)
		pass6DuplicateResource(m, reg, result)
		pass7HealthyOverride(m, reg, result)
		pass8UnsetVars(m, reg, result)
	}
	pass3DepRefs(m, result)
	pass4Cycles(m, result)

	return result
}

// GenericFallback captures one component that resolved to the generic
// provider fallback during Validate. The CLI uses these to emit per-
// component INFO logs (strict_types: false) or hard errors
// (strict_types: true).
type GenericFallback struct {
	Component string
	Type      string
}

// CollectGenericFallbacks walks every component and returns the ones
// whose type resolved to the built-in "generic" provider. Dedupes by
// component so each typo shows up once, not N times.
func CollectGenericFallbacks(m *Model, reg *providersupport.Registry) []GenericFallback {
	if reg == nil {
		return nil
	}
	var out []GenericFallback
	seen := make(map[string]bool, len(m.Order))
	for _, name := range m.Order {
		comp := m.Components[name]
		if comp == nil || comp.Type == "" {
			continue
		}
		_, providerName, err := comp.ResolveType(m, reg)
		if err != nil {
			continue
		}
		if providerName != providersupport.GenericProviderName {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, GenericFallback{Component: name, Type: comp.Type})
	}
	return out
}

func pass2TypeResolution(m *Model, reg *providersupport.Registry, result *ValidationResult) {
	for _, name := range m.Order {
		comp := m.Components[name]
		if comp.Type == "" {
			// pass1 already reported this; skip
			continue
		}
		_, providerName, err := comp.ResolveType(m, reg)
		if err != nil {
			result.Errors = append(result.Errors, ValidationError{
				Component: name,
				Field:     "type",
				Message:   fmt.Sprintf("type %s could not be resolved", comp.Type),
			})
			continue
		}
		// Strict opt-in: authors who set meta.strict_types: true reject
		// the generic fallback at validate time rather than discover it
		// silently at diagnose time.
		if m.Meta.StrictTypes && providerName == providersupport.GenericProviderName {
			result.Errors = append(result.Errors, ValidationError{
				Component: name,
				Field:     "type",
				Message:   fmt.Sprintf("type %q has no typed provider (strict_types: true rejects the generic fallback)", comp.Type),
			})
		}
	}
}

// pass5TriggeredBy warns when a state.triggered_by label isn't mentioned
// by any failure_modes.can_cause list in the registry. Typos produce
// states that can never fire, which is rarely intentional.
func pass5TriggeredBy(m *Model, reg *providersupport.Registry, result *ValidationResult) {
	producers := collectCanCauseLabels(reg, m)
	for _, name := range m.Order {
		comp := m.Components[name]
		if comp == nil || comp.Type == "" {
			continue
		}
		t, _, err := comp.ResolveType(m, reg)
		if err != nil || t == nil {
			continue
		}
		seen := make(map[string]bool)
		for _, st := range t.States {
			for _, label := range st.TriggeredBy {
				if producers[label] {
					continue
				}
				key := comp.Type + "\x00" + st.Name + "\x00" + label
				if seen[key] {
					continue
				}
				seen[key] = true
				result.Warnings = append(result.Warnings, ValidationWarning{
					Component: name,
					Field:     "triggered_by",
					Message:   fmt.Sprintf("type %q state %q: triggered_by label %q has no producer (no can_cause mentions it)", comp.Type, st.Name, label),
				})
			}
		}
	}
}

// collectCanCauseLabels builds the set of labels produced by any
// can_cause list across the registry + the model's own failure_modes.
func collectCanCauseLabels(reg *providersupport.Registry, m *Model) map[string]bool {
	out := map[string]bool{}
	if reg != nil {
		for _, p := range reg.All() {
			for _, t := range p.Types {
				for _, causes := range t.FailureModes {
					for _, label := range causes {
						out[label] = true
					}
				}
			}
		}
	}
	// Model-level failure_modes (component-scoped overrides) are also
	// producers — include them so a self-contained model doesn't warn
	// for its own labels.
	if m != nil {
		for _, comp := range m.Components {
			for _, causes := range comp.FailureModes {
				for _, label := range causes {
					out[label] = true
				}
			}
		}
	}
	return out
}

func pass1Structural(m *Model, result *ValidationResult) {
	if m.Meta.Name == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "meta.name",
			Message: "meta.name is required",
		})
	}
	if m.Meta.Version == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:   "meta.version",
			Message: "meta.version is required",
		})
	}
	for _, name := range m.Order {
		comp := m.Components[name]
		if comp.Type == "" {
			result.Errors = append(result.Errors, ValidationError{
				Component: name,
				Field:     "type",
				Message:   fmt.Sprintf("component %q has no type", name),
			})
		}
	}
}

func pass3DepRefs(m *Model, result *ValidationResult) {
	for _, name := range m.Order {
		comp := m.Components[name]
		for _, dep := range comp.Depends {
			if dep.Need < 0 || dep.Need > len(dep.On) {
				result.Errors = append(result.Errors, ValidationError{
					Component: name,
					Field:     "depends",
					Message:   fmt.Sprintf("need: %d over %d component(s) [%s]: want 1 to %d", dep.Need, len(dep.On), strings.Join(dep.On, ", "), len(dep.On)),
				})
			}
			for _, target := range dep.On {
				if _, ok := m.Components[target]; !ok {
					suggestion := closestMatch(target, m.Components)
					result.Errors = append(result.Errors, ValidationError{
						Component:  name,
						Field:      "depends",
						Message:    fmt.Sprintf("component %q depends on unknown component %q", name, target),
						Suggestion: suggestion,
					})
				}
			}
		}
	}
}

// closestMatch returns the component name from candidates that has the
// smallest Levenshtein distance to target, or "" if candidates is empty.
func closestMatch(target string, candidates map[string]*Component) string {
	best := ""
	bestDist := -1
	for name := range candidates {
		d := levenshtein(target, name)
		if bestDist < 0 || d < bestDist {
			bestDist = d
			best = name
		}
	}
	return best
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	ra := []rune(strings.ToLower(a))
	rb := []rune(strings.ToLower(b))
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	// dp[i][j] = edit distance between ra[:i] and rb[:j]
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
	}
	for i := 0; i <= la; i++ {
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := dp[i-1][j] + 1
			ins := dp[i][j-1] + 1
			sub := dp[i-1][j-1] + cost
			dp[i][j] = min(del, ins, sub)
		}
	}
	return dp[la][lb]
}

// pass6DuplicateResource warns when two components share the same
// (owning-provider, type, resource) triple — almost always a
// copy-paste mistake. Components with empty Resource are skipped:
// they fall back to Name at probe time, and key uniqueness is
// already enforced by pass1Structural.
func pass6DuplicateResource(m *Model, reg *providersupport.Registry, result *ValidationResult) {
	type seenKey struct{ owner, typ, resource string }
	seen := map[seenKey]string{} // first component owning each triple

	for _, name := range m.Order {
		comp := m.Components[name]
		if comp == nil || comp.Resource == "" {
			continue
		}
		_, owner, err := comp.ResolveType(m, reg)
		if err != nil {
			continue
		}
		key := seenKey{owner: owner, typ: comp.Type, resource: comp.Resource}
		if prior, ok := seen[key]; ok {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Component: name,
				Field:     "resource",
				Message:   fmt.Sprintf("duplicate resource %q — same (provider %q, type %q) as component %q", comp.Resource, owner, comp.Type, prior),
			})
			continue
		}
		seen[key] = name
	}
}

// pass7HealthyOverride warns, rule by rule, when a component's healthy:
// list leaves out one of its type's default rules. The list replaces the
// type's rules rather than adding to them, so a rule not restated is
// gone: an RDS override without `available == true` calls a stopped
// database healthy. Rules compare with whitespace ignored.
func pass7HealthyOverride(m *Model, reg *providersupport.Registry, result *ValidationResult) {
	squash := func(r string) string { return strings.Join(strings.Fields(r), "") }
	for _, name := range m.Order {
		comp := m.Components[name]
		// replace: says the drop is deliberate; add: drops nothing.
		if comp == nil || len(comp.HealthyRaw) == 0 || comp.HealthyMode != "" {
			continue
		}
		t, _, err := comp.ResolveType(m, reg)
		if err != nil || t == nil {
			continue
		}
		kept := make(map[string]bool, len(comp.HealthyRaw))
		for _, r := range comp.HealthyRaw {
			kept[squash(r)] = true
		}
		for _, r := range t.HealthyRaw {
			if kept[squash(r)] {
				continue
			}
			result.Warnings = append(result.Warnings, ValidationWarning{
				Component: name,
				Field:     "healthy",
				Message:   fmt.Sprintf("healthy override drops type rule %q (the override replaces the %s rules; restate it to keep it)", r, comp.Type),
			})
		}
	}
}

// pass8UnsetVars warns when a component's rules compare against a var --
// `restart_count <= max_restart_count` -- that nothing sets: not the
// component's vars:, not meta.vars:, not a provider default. Such a rule
// can never be decided, so the component can never be shown healthy.
func pass8UnsetVars(m *Model, reg *providersupport.Registry, result *ValidationResult) {
	vars := m.VarLookup(reg)
	for _, name := range m.Order {
		comp := m.Components[name]
		if comp == nil {
			continue
		}
		t, _, err := comp.ResolveType(m, reg)
		if err != nil || t == nil {
			continue
		}
		rules := comp.HealthyRules(t)
		for _, st := range t.States {
			if st.When != nil {
				rules = append(rules[:len(rules):len(rules)], st.When)
			}
		}
		warned := map[string]bool{}
		for _, rule := range rules {
			for _, key := range varRefs(rule, t) {
				if _, set := vars.LookupVar(name, key); set || warned[key] {
					continue
				}
				warned[key] = true
				result.Warnings = append(result.Warnings, ValidationWarning{
					Component: name,
					Field:     "vars",
					Message:   fmt.Sprintf("%s rules compare against %q, which is not set (component vars:, meta.vars: or a provider default); those rules can never be decided", comp.Type, key),
				})
			}
		}
	}
}

// varRefs returns the bare words n compares t's non-string facts against
// that are not themselves facts of t: the vars the evaluator will look up.
func varRefs(n expr.Node, t *providersupport.Type) []string {
	var out []string
	var walk func(expr.Node)
	walk = func(n expr.Node) {
		switch v := n.(type) {
		case *expr.AndNode:
			walk(v.L)
			walk(v.R)
		case *expr.OrNode:
			walk(v.L)
			walk(v.R)
		case *expr.CmpNode:
			s, ok := v.Value.(string)
			if !ok || v.Component != "" || v.Fact == "" || v.Fact == "state" || !isBareWord(s) {
				return
			}
			if f := t.Facts[v.Fact]; f != nil && f.TypeName == "mgtt.string" {
				return // a literal, as in phase == Bound
			}
			if _, isFact := t.Facts[s]; !isFact {
				out = append(out, s)
			}
		}
	}
	walk(n)
	return out
}

func isBareWord(s string) bool {
	for i, c := range s {
		if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9' || i == 0) {
			return false
		}
	}
	return s != "" && s != "true" && s != "false"
}

func pass4Cycles(m *Model, result *ValidationResult) {
	if m.graph == nil {
		m.BuildGraph()
	}
	cycle := m.graph.DetectCycle()
	if len(cycle) > 0 {
		result.Errors = append(result.Errors, ValidationError{
			Component: "",
			Field:     "depends",
			Message:   fmt.Sprintf("circular dependency detected: %s", strings.Join(cycle, " → ")),
		})
	}
}
