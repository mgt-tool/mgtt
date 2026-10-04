// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestAbout_ReturnsServerMetadata(t *testing.T) {
	h := NewHandler(Config{})
	result, err := h.About()
	if err != nil {
		t.Fatalf("About() returned error: %v", err)
	}
	if result.Version == "" {
		t.Error("expected non-empty version")
	}
	if len(result.Transports) == 0 {
		t.Error("expected at least one transport reported")
	}
	if _, err := json.Marshal(result); err != nil {
		t.Errorf("About result not JSON-serialisable: %v", err)
	}
}

// Every tool name is portable by default: some clients and model APIs
// reject a whole tool list over one name outside [A-Za-z0-9_-]{1,64}.
func TestBuildServer_ToolNamesArePortable(t *testing.T) {
	portable := regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	tools := buildServer(Config{}).ListTools()
	if len(tools) == 0 {
		t.Fatal("no tools registered")
	}
	for name := range tools {
		if !portable.MatchString(name) {
			t.Errorf("tool name %q is not portable", name)
		}
	}
	for _, legacy := range legacyToolNames {
		if _, ok := tools[legacy]; ok {
			t.Errorf("legacy name %q registered without --legacy-tool-names", legacy)
		}
	}
}

// --legacy-tool-names adds each dotted name as an alias that runs the
// same handler as the tool it was renamed to.
func TestBuildServer_LegacyAliases(t *testing.T) {
	tools := buildServer(Config{LegacyToolNames: true}).ListTools()
	for name, legacy := range legacyToolNames {
		alias, ok := tools[legacy]
		if !ok {
			t.Errorf("alias %q for %q not registered", legacy, name)
			continue
		}
		if !strings.HasPrefix(alias.Tool.Description, "Deprecated alias of "+name) {
			t.Errorf("alias %q description does not say what replaces it: %q", legacy, alias.Tool.Description)
		}
		if _, ok := tools[name]; !ok {
			t.Errorf("canonical %q missing when aliases are on", name)
		}
	}
}
