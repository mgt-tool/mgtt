// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package probe

import (
	"fmt"
	"regexp"
	"strings"
)

var placeholderRE = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// Substitute replaces template placeholders in a command string.
//
//   - {name}      → component
//   - {namespace} → modelVars["namespace"]
//   - {varName}   → modelVars[varName] or providerVars[varName]
//
// Lookup order: modelVars takes precedence over providerVars. Unknown
// placeholders are left intact.
func Substitute(template, component string, modelVars, providerVars map[string]string) string {
	return placeholderRE.ReplaceAllStringFunc(template, func(match string) string {
		key := match[1 : len(match)-1]
		if key == "name" {
			return component
		}
		if v, ok := modelVars[key]; ok {
			return v
		}
		if v, ok := providerVars[key]; ok {
			return v
		}
		return match
	})
}

// ValidateCommand checks that the rendered command does not contain shell
// metacharacters that were not already present in the original template.
// Guards against command injection through variable substitution.
func ValidateCommand(rendered, template string) error {
	metacharacters := []string{";", "&&", "||", "|", "$(", "`", ">>", "<<", ">", "<", "\n"}
	for _, meta := range metacharacters {
		if strings.Count(rendered, meta) > strings.Count(template, meta) {
			return fmt.Errorf("injection detected: %q appears more often in rendered command than in template", meta)
		}
	}
	return nil
}
