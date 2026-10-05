// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

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
    "probe_timeout_seconds":{"type":"integer"},
    "toolset":{"type":"string","enum":["all","diagnose","authoring"]}
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
    "saved":{"type":"boolean"},
    "scenario_path":{"type":"string"},
    "scenario_yaml":{"type":"string"},
    "scenario_passes":{"type":"boolean"},
    "scenario_warning":{"type":"string"}
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
          "note":{"type":"string"},
          "status":{"type":"string","enum":["","not_found","forbidden","transient"]}
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
    "root_cause":{"type":"string"},
    "redundancy_degraded":{"type":"array","items":{"type":"string"}},
    "cannot_rule_out":{"type":"array","items":{"type":"object","properties":{"component":{"type":"string"},"facts":{"type":"object","additionalProperties":{"type":"string","enum":["forbidden","transient"]}}},"required":["component","facts"]}}
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
        "eliminates_count":{"type":"integer"},
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
    "status":{"type":"string","enum":["rendered","executed","not_found","forbidden","transient","operator_prompt_required","no_suggestion","error","blocked_readonly","blocked_write_fail","blocked_write_pause","blocked_budget"]},
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

// ScenariosListOutputSchema is shared by scenarios_list and scenarios_alive.
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
          "observations":{"type":"array","items":{"type":"string"}},
          "count":{"type":"integer"}
        },
        "required":["id","root","chain"]
      }
    },
    "chains":{"type":"integer"},
    "total":{"type":"integer"},
    "next_page_token":{"type":"string"}
  },
  "required":["scenarios","chains","total"]
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
    "surviving_chains":{"type":"integer"},
    "surviving_classes":{"type":"integer"},
    "eliminated_chains":{"type":"integer"},
    "eliminated_classes":{"type":"integer"},
    "facts":{"type":"array"},
    "suggested_next":{"type":"object"},
    "verdict":{"type":"string"},
    "cannot_rule_out":{"type":"array","items":{"type":"object","properties":{"component":{"type":"string"},"facts":{"type":"object","additionalProperties":{"type":"string","enum":["forbidden","transient"]}}},"required":["component","facts"]}}
  },
  "required":["incident_id","model_ref","started_at","status","entry_point","surviving_scenarios","eliminated_scenarios","surviving_chains","surviving_classes","eliminated_chains","eliminated_classes","facts"]
}`

// ScenarioSuggestOutputSchema describes one page of drafted scenarios.
const ScenarioSuggestOutputSchema = `{
  "type":"object",
  "properties":{
    "drafts":{"type":"array","items":{"type":"object","properties":{
      "name":{"type":"string"},
      "chain":{"type":"array","items":{"type":"string"}},
      "count":{"type":"integer"},
      "root_cause":{"type":"string"},
      "review":{"type":"string"},
      "yaml":{"type":"string"}},
      "required":["name","root_cause","yaml"]}},
    "chains":{"type":"integer"},
    "total":{"type":"integer"},
    "next_page_token":{"type":"string"},
    "unshowable":{"type":"array","items":{"type":"string"}}
  },
  "required":["drafts","chains","total"]
}`

// TypesListOutputSchema describes the installed vocabulary listing.
const TypesListOutputSchema = `{
  "type":"object",
  "properties":{
    "types":{"type":"array","items":{"type":"object","properties":{
      "provider":{"type":"string"},"type":{"type":"string"},
      "description":{"type":"string"},"facts":{"type":"integer"}},
      "required":["provider","type","facts"]}}
  },
  "required":["types"]
}`

// TypesDescribeOutputSchema describes one type.
const TypesDescribeOutputSchema = `{
  "type":"object",
  "properties":{
    "provider":{"type":"string"},
    "type":{"type":"string"},
    "description":{"type":"string"},
    "facts":{"type":"array","items":{"type":"object","properties":{
      "name":{"type":"string"},"type":{"type":"string"},"cost":{"type":"string"},"access":{"type":"string"},
      "window":{"type":"string"},"derive":{"type":"string","enum":["delta","rate","max"]}},
      "required":["name"]}},
    "healthy":{"type":"array","items":{"type":"string"}},
    "states":{"type":"array","items":{"type":"object","properties":{
      "name":{"type":"string"},"when":{"type":"string"},"description":{"type":"string"},
      "triggered_by":{"type":"array","items":{"type":"string"}},
      "can_cause":{"type":"array","items":{"type":"string"}}},
      "required":["name"]}},
    "default_active_state":{"type":"string"},
    "variables":{"type":"array","items":{"type":"object","properties":{
      "name":{"type":"string"},"description":{"type":"string"},
      "required":{"type":"boolean"},"default":{"type":"string"}},
      "required":["name"]}}
  },
  "required":["provider","type","facts","healthy","states"]
}`

// ModelValidateOutputSchema describes a validation report.
const ModelValidateOutputSchema = `{
  "type":"object",
  "properties":{
    "ok":{"type":"boolean"},
    "components":{"type":"integer"},
    "errors":{"type":"array","items":{"$ref":"#/$defs/finding"}},
    "warnings":{"type":"array","items":{"$ref":"#/$defs/finding"}}
  },
  "required":["ok","components","errors","warnings"],
  "$defs":{
    "finding":{"type":"object","properties":{
      "component":{"type":"string"},"field":{"type":"string"},
      "message":{"type":"string"},"suggestion":{"type":"string"}},
      "required":["field","message"]}
  }
}`

// ScenarioSimulateOutputSchema describes a simulation report.
const ScenarioSimulateOutputSchema = `{
  "type":"object",
  "properties":{
    "passed":{"type":"integer"},
    "failed":{"type":"integer"},
    "results":{"type":"array","items":{"type":"object","properties":{
      "name":{"type":"string"},"pass":{"type":"boolean"},
      "expected":{"$ref":"#/$defs/conclusion"},"actual":{"$ref":"#/$defs/conclusion"},
      "error":{"type":"string"}},
      "required":["name","pass","expected","actual"]}}
  },
  "required":["passed","failed","results"],
  "$defs":{
    "conclusion":{"type":"object","properties":{
      "root_cause":{"type":"string"},
      "path":{"type":"array","items":{"type":"string"}},
      "eliminated":{"type":"array","items":{"type":"string"}},
      "not_eliminated":{"type":"array","items":{"type":"string"}},
      "cannot_rule_out":{"type":"array","items":{"type":"string"}},
      "redundancy_degraded":{"type":"array","items":{"type":"string"}}},
      "required":["root_cause"]}
  }
}`

// GuideOutputSchema describes a guide topic.
const GuideOutputSchema = `{
  "type":"object",
  "properties":{
    "topic":{"type":"string"},
    "text":{"type":"string"},
    "topics":{"type":"array","items":{"type":"string"}}
  },
  "required":["topic","text","topics"]
}`

// ModelImpactOutputSchema describes a blast radius.
const ModelImpactOutputSchema = `{
  "type":"object",
  "properties":{
    "component":{"type":"string"},
    "states":{"type":"array","items":{"type":"string"}},
    "affected":{"type":"array","items":{"type":"object","properties":{
      "component":{"type":"string"},
      "states":{"type":"array","items":{"type":"string"}},
      "path":{"type":"array","items":{"type":"string"}},
      "symptom":{"type":"boolean"},
      "conditions":{"type":"array","items":{"type":"string"}}},
      "required":["component","states","path"]}},
    "symptoms":{"type":"array","items":{"type":"string"}},
    "blocked":{"type":"array","items":{"type":"object","properties":{
      "dependent":{"type":"string"},"member":{"type":"string"}},
      "required":["dependent","member"]}}
  },
  "required":["component","states","affected","symptoms","blocked"]
}`

// ModelDiffOutputSchema describes a semantic model diff.
const ModelDiffOutputSchema = `{
  "type":"object",
  "properties":{
    "same":{"type":"boolean"},
    "added":{"type":"array","items":{"type":"string"}},
    "removed":{"type":"array","items":{"type":"string"}},
    "changed":{"type":"array","items":{"type":"object","properties":{
      "component":{"type":"string"},"changes":{"type":"array","items":{"type":"string"}}},
      "required":["component","changes"]}},
    "reach":{"type":"array","items":{"type":"object","properties":{
      "component":{"type":"string"},
      "gained":{"type":"array","items":{"type":"string"}},
      "lost":{"type":"array","items":{"type":"string"}}},
      "required":["component"]}},
    "old_scenarios":{"type":"integer"},
    "new_scenarios":{"type":"integer"}
  },
  "required":["same","added","removed","changed","reach","old_scenarios","new_scenarios"]
}`

// ModelDiscoverOutputSchema describes a discovery proposal.
const ModelDiscoverOutputSchema = `{
  "type":"object",
  "properties":{
    "proposed_model":{"type":"string"},
    "discovered":{"type":"array","items":{"type":"object","properties":{
      "provider":{"type":"string"},"components":{"type":"integer"},"dependencies":{"type":"integer"}},
      "required":["provider","components","dependencies"]}},
    "failures":{"type":"object","additionalProperties":{"type":"string"}},
    "added":{"type":"array","items":{"type":"string"}},
    "removed":{"type":"array","items":{"type":"string"}},
    "kept_authored":{"type":"array","items":{"type":"string"}},
    "dangling":{"type":"array","items":{"type":"string"}}
  },
  "required":["proposed_model","discovered","added","removed","kept_authored","dangling"]
}`
