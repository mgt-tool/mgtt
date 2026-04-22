package mcp

import (
	"encoding/json"
	"testing"
)

// TestOutputSchemas_ParseAsJSON catches any typo or stray character
// introduced into an output schema constant BEFORE agents see it.
// Output schemas are part of the public tool contract; a malformed
// one breaks every agent that depends on the response shape.
//
// The schemas are also wired into each tool registration via
// mcpgo.WithRawOutputSchema (see server.go rawOutput). Should that
// wiring be removed, the constants are still exercised here and the
// "unused symbol" maintenance trap the code review flagged is gone.
func TestOutputSchemas_ParseAsJSON(t *testing.T) {
	cases := map[string]string{
		"about":             AboutOutputSchema,
		"incident.start":    IncidentStartOutputSchema,
		"incident.end":      IncidentEndOutputSchema,
		"fact.add":          FactAddOutputSchema,
		"facts.list":        FactsListOutputSchema,
		"plan":              PlanOutputSchema,
		"probe":             ProbeOutputSchema,
		"scenarios.list":    ScenariosListOutputSchema,
		"incident.snapshot": IncidentSnapshotOutputSchema,
	}
	for tool, schema := range cases {
		if schema == "" {
			t.Errorf("%s: schema constant is empty", tool)
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(schema), &decoded); err != nil {
			t.Errorf("%s: schema does not parse as JSON: %v", tool, err)
			continue
		}
		// Every MCP tool-output schema is a JSON Schema object with
		// at least a "type" and "properties" key. Catch the "pasted
		// wrong shape" mistake early.
		if decoded["type"] != "object" {
			t.Errorf("%s: root type is not object", tool)
		}
		if _, ok := decoded["properties"]; !ok {
			t.Errorf("%s: missing properties", tool)
		}
	}
}
