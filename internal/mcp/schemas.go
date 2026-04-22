package mcp

// AboutInputSchema — no parameters.
const AboutInputSchema = `{"type":"object","properties":{},"additionalProperties":false}`

// AboutOutputSchema — documents the AboutResult shape.
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

// IncidentStartInputSchema documents the input shape. model_ref is required;
// id and suspect are optional.
const IncidentStartInputSchema = `{
  "type":"object",
  "properties":{
    "model_ref":{"type":"string","description":"path to system.model.yaml the server can read"},
    "id":{"type":"string","description":"optional incident id; generated if omitted"},
    "suspect":{"type":"array","items":{"type":"string"},"description":"optional component.state hints, e.g. [\"api.crash_looping\"]"}
  },
  "required":["model_ref"],
  "additionalProperties":false
}`

// IncidentStartOutputSchema documents the IncidentStartResult shape.
const IncidentStartOutputSchema = `{
  "type":"object",
  "properties":{
    "incident_id":{"type":"string"}
  },
  "required":["incident_id"]
}`

// IncidentEndInputSchema documents the input. incident_id required;
// verdict optional.
const IncidentEndInputSchema = `{
  "type":"object",
  "properties":{
    "incident_id":{"type":"string"},
    "verdict":{"type":"string"}
  },
  "required":["incident_id"],
  "additionalProperties":false
}`

// IncidentEndOutputSchema documents the IncidentEndResult shape.
const IncidentEndOutputSchema = `{
  "type":"object",
  "properties":{
    "saved":{"type":"boolean"}
  },
  "required":["saved"]
}`

// FactAddInputSchema — incident_id/component/key/value required, note
// optional. Value is any JSON type (string, number, bool, etc.).
const FactAddInputSchema = `{
  "type":"object",
  "properties":{
    "incident_id":{"type":"string"},
    "component":{"type":"string"},
    "key":{"type":"string"},
    "value":{},
    "note":{"type":"string"}
  },
  "required":["incident_id","component","key","value"],
  "additionalProperties":false
}`

// FactAddOutputSchema confirms the append.
const FactAddOutputSchema = `{
  "type":"object",
  "properties":{"appended":{"type":"boolean"}},
  "required":["appended"]
}`

// FactsListInputSchema — incident_id required; component optional filter.
const FactsListInputSchema = `{
  "type":"object",
  "properties":{
    "incident_id":{"type":"string"},
    "component":{"type":"string"}
  },
  "required":["incident_id"],
  "additionalProperties":false
}`

// PlanInputSchema — incident_id required, component optional.
const PlanInputSchema = `{
  "type":"object",
  "properties":{
    "incident_id":{"type":"string"},
    "component":{"type":"string"}
  },
  "required":["incident_id"],
  "additionalProperties":false
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

// ProbeInputSchema — incident_id required, execute required, component optional.
const ProbeInputSchema = `{
  "type":"object",
  "properties":{
    "incident_id":{"type":"string"},
    "component":{"type":"string"},
    "execute":{"type":"boolean"}
  },
  "required":["incident_id","execute"],
  "additionalProperties":false
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

// ScenariosListInputSchema — incident_id required.
const ScenariosListInputSchema = `{
  "type":"object",
  "properties":{"incident_id":{"type":"string"}},
  "required":["incident_id"],
  "additionalProperties":false
}`

// ScenariosListOutputSchema — shared by scenarios.list and scenarios.alive.
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

// IncidentSnapshotInputSchema — incident_id required; selectors deferred.
const IncidentSnapshotInputSchema = `{
  "type":"object",
  "properties":{"incident_id":{"type":"string"}},
  "required":["incident_id"],
  "additionalProperties":false
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
