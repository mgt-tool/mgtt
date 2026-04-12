package probe

import (
	"fmt"
	"strings"
)

// Substitute replaces template placeholders in a command string.
//
//   - {name}      → component
//   - {namespace} → modelVars["namespace"]
//   - {varName}   → modelVars[varName] or providerVars[varName]
//
// Lookup order: modelVars takes precedence over providerVars.
func Substitute(template, component string, modelVars map[string]string, providerVars map[string]string) string {
	result := template

	// Replace {name} with the component name.
	result = strings.ReplaceAll(result, "{name}", component)

	// Replace all other {varName} placeholders.
	result = replacePlaceholders(result, component, modelVars, providerVars)

	return result
}

// replacePlaceholders scans for {…} tokens and substitutes them.
func replacePlaceholders(s, component string, modelVars, providerVars map[string]string) string {
	var b strings.Builder
	b.Grow(len(s))

	i := 0
	for i < len(s) {
		open := strings.Index(s[i:], "{")
		if open == -1 {
			b.WriteString(s[i:])
			break
		}
		open += i
		b.WriteString(s[i:open])

		close := strings.Index(s[open:], "}")
		if close == -1 {
			// No matching close — write rest as-is.
			b.WriteString(s[open:])
			break
		}
		close += open

		key := s[open+1 : close]

		// {name} is already handled in Substitute, but handle it here too for
		// safety when called directly.
		if key == "name" {
			b.WriteString(component)
		} else if val, ok := modelVars[key]; ok {
			b.WriteString(val)
		} else if providerVars != nil {
			if val, ok := providerVars[key]; ok {
				b.WriteString(val)
			} else {
				// Unknown placeholder — leave as-is.
				b.WriteString(s[open : close+1])
			}
		} else {
			// Unknown placeholder — leave as-is.
			b.WriteString(s[open : close+1])
		}

		i = close + 1
	}

	return b.String()
}

// ValidateCommand checks that the rendered command does not contain shell
// metacharacters that were not already present in the original template.
// This guards against command injection through variable substitution.
func ValidateCommand(rendered, template string) error {
	metacharacters := []string{";", "&&", "||", "|", "$(", "`"}

	for _, meta := range metacharacters {
		// Count occurrences in template vs rendered.
		templateCount := strings.Count(template, meta)
		renderedCount := strings.Count(rendered, meta)

		if renderedCount > templateCount {
			return fmt.Errorf("injection detected: %q appears %d times in rendered command but only %d times in template",
				meta, renderedCount, templateCount)
		}
	}

	return nil
}
