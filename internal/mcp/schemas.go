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
