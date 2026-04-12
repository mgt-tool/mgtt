package model

import (
	"fmt"
	"strings"

	"github.com/sajonaro/mgtt/internal/providersupport"
)

// Validate runs all validation passes against the loaded model and returns a
// ValidationResult. When reg is non-nil, pass 2 resolves component types
// against the provider registry.
func Validate(m *Model, reg *providersupport.Registry) *ValidationResult {
	result := &ValidationResult{}

	pass1Structural(m, result)
	if reg != nil {
		pass2TypeResolution(m, reg, result)
	}
	pass3DepRefs(m, result)
	pass4Cycles(m, result)

	return result
}

// ---------------------------------------------------------------------------
// Pass 2 — Type resolution
// ---------------------------------------------------------------------------

func pass2TypeResolution(m *Model, reg *providersupport.Registry, result *ValidationResult) {
	for _, name := range m.Order {
		comp := m.Components[name]
		if comp.Type == "" {
			// pass1 already reported this; skip
			continue
		}
		// Determine which providers to resolve against: component-level or meta-level.
		providers := comp.Providers
		if len(providers) == 0 {
			providers = m.Meta.Providers
		}
		if _, _, err := reg.ResolveType(providers, comp.Type); err != nil {
			result.Errors = append(result.Errors, ValidationError{
				Component: name,
				Field:     "type",
				Message:   fmt.Sprintf("type %s could not be resolved", comp.Type),
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Pass 1 — Structural
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Pass 3 — Dependency references
// ---------------------------------------------------------------------------

func pass3DepRefs(m *Model, result *ValidationResult) {
	for _, name := range m.Order {
		comp := m.Components[name]
		for _, dep := range comp.Depends {
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
			dp[i][j] = min3(del, ins, sub)
		}
	}
	return dp[la][lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// ---------------------------------------------------------------------------
// Pass 4 — Cycle detection
// ---------------------------------------------------------------------------

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
