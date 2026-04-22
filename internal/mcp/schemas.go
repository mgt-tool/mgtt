// Package mcp — output schemas for every tool the server registers.
//
// Input schemas are not declared here: mcp-go's fluent tool builder
// (mcpgo.WithString, WithBoolean, etc.) generates an accurate input
// schema from the registration itself, so a separate constant would
// be redundant and drift-prone.
//
// Output schemas are not auto-derived by the SDK, so they live here
// and are wired into each tool via WithRawOutputSchema. An agent that
// calls tools/list sees the exact shape its consumer-side decoder
// should expect.
//
// Drift guard: `schemas_test.go` parses every constant as JSON to
// catch a typo before it ships. Changing these is a public contract
// change — bump at least a minor version.
package mcp

// AboutOutputSchema documents the AboutResult shape.
const AboutOutputSchema = `{
  "type":"object",
  "properties":{
    "version":{"type":"string"},
    "transports":{"type":"array","items":{"type":"string"}},
    "readonly_only":{"type":"boolean"},
    "on_write":{"type":"string","enum":["pause","run","fail"]},
    "max_execute_per_incident":{"type":"integer"},
    "probe_timeout_seconds":{"type":"integer"}
  },
  "required":["version","transports","readonly_only","on_write","max_execute_per_incident","probe_timeout_seconds"]
}`

// IncidentStartOutputSchema documents the IncidentStartResult shape.
const IncidentStartOutputSchema = `{
  "type":"object",
  "properties":{
    "incident_id":{"type":"string"}
  },
  "required":["incident_id"]
}`

// IncidentEndOutputSchema documents the IncidentEndResult shape.
const IncidentEndOutputSchema = `{
  "type":"object",
  "properties":{
    "saved":{"type":"boolean"}
  },
  "required":["saved"]
}`

// FactAddOutputSchema confirms the append.
const FactAddOutputSchema = `{
  "type":"object",
  "properties":{"appended":{"type":"boolean"}},
  "required":["appended"]
}`

// FactsListOutputSchema documents the flat fact-entry list.
const FactsListOutputSchema = `{
  "type":"object",
  "properties":{
    "facts":{
      "type":"array",
      "items":{
        "type":"object",
        "properties":{
          "component":{"type":"string"},
          "key":{"type":"string"},
          "value":{},
          "at":{"type":"string","format":"date-time"},
          "collector":{"type":"string"},
          "note":{"type":"string"}
        },
        "required":["component","key","at","collector"]
      }
    }
  },
  "required":["facts"]
}`

// PlanOutputSchema documents the path tree + suggestion.
const PlanOutputSchema = `{
  "type":"object",
  "properties":{
    "entry":{"type":"string"},
    "paths":{"type":"array","items":{"$ref":"#/$defs/path"}},
    "eliminated":{"type":"array","items":{"$ref":"#/$defs/path"}},
    "suggested":{"$ref":"#/$defs/suggested"},
    "root_cause":{"type":"string"}
  },
  "required":["entry","paths"],
  "$defs":{
    "path":{
      "type":"object",
      "properties":{
        "id":{"type":"string"},
        "components":{"type":"array","items":{"type":"string"}},
        "reason":{"type":"string"}
      },
      "required":["id","components"]
    },
    "suggested":{
      "type":"object",
      "properties":{
        "component":{"type":"string"},
        "fact":{"type":"string"},
        "provider":{"type":"string"},
        "eliminates":{"type":"array","items":{"type":"string"}},
        "cost":{"type":"string"},
        "access":{"type":"string"},
        "rendered_command":{"type":"string"}
      },
      "required":["component","fact"]
    }
  }
}`

// ProbeOutputSchema documents the discriminated status envelope.
const ProbeOutputSchema = `{
  "type":"object",
  "properties":{
    "status":{"type":"string","enum":["rendered","executed","not_found","operator_prompt_required","no_suggestion","error","blocked_readonly","blocked_write_fail","blocked_write_pause","blocked_budget"]},
    "component":{"type":"string"},
    "fact":{"type":"string"},
    "provider":{"type":"string"},
    "rendered_command":{"type":"string"},
    "value":{},
    "raw":{"type":"string"},
    "reason":{"type":"string"}
  },
  "required":["status"]
}`

// ScenariosListOutputSchema is shared by scenarios.list and scenarios.alive.
const ScenariosListOutputSchema = `{
  "type":"object",
  "properties":{
    "scenarios":{
      "type":"array",
      "items":{
        "type":"object",
        "properties":{
          "id":{"type":"string"},
          "root":{
            "type":"object",
            "properties":{"component":{"type":"string"},"state":{"type":"string"}},
            "required":["component","state"]
          },
          "chain":{
            "type":"array",
            "items":{
              "type":"object",
              "properties":{
                "component":{"type":"string"},
                "state":{"type":"string"},
                "emits_on_edge":{"type":"string"},
                "observes":{"type":"array","items":{"type":"string"}}
              },
              "required":["component","state"]
            }
          },
          "observations":{"type":"array","items":{"type":"string"}}
        },
        "required":["id","root","chain"]
      }
    }
  },
  "required":["scenarios"]
}`

// IncidentSnapshotOutputSchema describes the full diagnostic-memory bundle.
const IncidentSnapshotOutputSchema = `{
  "type":"object",
  "properties":{
    "incident_id":{"type":"string"},
    "model_ref":{
      "type":"object",
      "properties":{"path":{"type":"string"},"sha256":{"type":"string"}},
      "required":["path"]
    },
    "started_at":{"type":"string","format":"date-time"},
    "ended_at":{"type":"string","format":"date-time"},
    "status":{"type":"string","enum":["open","closed"]},
    "entry_point":{"type":"string"},
    "surviving_scenarios":{"type":"array"},
    "eliminated_scenarios":{"type":"array"},
    "facts":{"type":"array"},
    "suggested_next":{"type":"object"},
    "verdict":{"type":"string"}
  },
  "required":["incident_id","model_ref","started_at","status","entry_point","surviving_scenarios","eliminated_scenarios","facts"]
}`
