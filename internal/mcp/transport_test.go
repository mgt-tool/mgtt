package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// TestTransport_FullDiagnosisLoopOverMCP is the design's §9 layer-4 test:
// start a real MCP server, drive it through the client transport, and
// round-trip a complete diagnosis loop. Uses mcp-go's in-process client
// so the transport exercise is real (initialize, tool call, JSON
// envelope, result parse) without a subprocess.
func TestTransport_FullDiagnosisLoopOverMCP(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MGTT_HOME", dir)
	modelPath := writeMinimalModel(t, dir)

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	s := buildServer(Config{Version: "e2e"})
	client, err := mcpclient.NewInProcessClient(s)
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client.Start: %v", err)
	}
	if _, err := client.Initialize(ctx, mcpgo.InitializeRequest{}); err != nil {
		t.Fatalf("client.Initialize: %v", err)
	}

	// 1. about — discovery.
	var about AboutResult
	mustCallTool(t, client, "about", nil, &about)
	if about.Version != "e2e" {
		t.Errorf("about.version: got %q want %q", about.Version, "e2e")
	}

	// 2. incident.start
	var startResult IncidentStartResult
	mustCallTool(t, client, "incident.start", map[string]any{
		"model_ref": modelPath,
		"id":        "e2e-incident",
	}, &startResult)
	if startResult.IncidentID != "e2e-incident" {
		t.Fatalf("incident_id: got %q", startResult.IncidentID)
	}

	// 3. fact.add — the agent reports an operator observation.
	var addResult FactAddResult
	mustCallTool(t, client, "fact.add", map[string]any{
		"incident_id": startResult.IncidentID,
		"component":   "api",
		"key":         "operator_says_healthy",
		"value":       true,
	}, &addResult)
	if !addResult.Appended {
		t.Error("fact.add should report appended: true")
	}

	// 4. facts.list — round-trip.
	var listResult FactsListResult
	mustCallTool(t, client, "facts.list", map[string]any{
		"incident_id": startResult.IncidentID,
	}, &listResult)
	if len(listResult.Facts) != 1 {
		t.Fatalf("facts: got %d want 1", len(listResult.Facts))
	}

	// 5. plan — with the healthy fact recorded, no suggestion remains.
	var planResult PlanResult
	mustCallTool(t, client, "plan", map[string]any{
		"incident_id": startResult.IncidentID,
	}, &planResult)
	if planResult.Suggested != nil {
		t.Errorf("plan: expected no suggestion after healthy fact, got %+v", planResult.Suggested)
	}

	// 6. incident.snapshot — full bundle.
	var snap IncidentSnapshotResult
	mustCallTool(t, client, "incident.snapshot", map[string]any{
		"incident_id": startResult.IncidentID,
	}, &snap)
	if snap.Status != "open" {
		t.Errorf("snapshot.status: got %q want open", snap.Status)
	}
	if len(snap.Facts) != 1 {
		t.Errorf("snapshot.facts: got %d want 1", len(snap.Facts))
	}

	// 7. incident.end with verdict.
	var endResult IncidentEndResult
	mustCallTool(t, client, "incident.end", map[string]any{
		"incident_id": startResult.IncidentID,
		"verdict":     "operator confirmed healthy",
	}, &endResult)
	if !endResult.Saved {
		t.Error("incident.end should report saved: true")
	}

	// 8. Second snapshot reflects closed + verdict.
	var closed IncidentSnapshotResult
	mustCallTool(t, client, "incident.snapshot", map[string]any{
		"incident_id": startResult.IncidentID,
	}, &closed)
	if closed.Status != "closed" {
		t.Errorf("status after end: got %q want closed", closed.Status)
	}
	if closed.Verdict != "operator confirmed healthy" {
		t.Errorf("verdict: got %q", closed.Verdict)
	}
}

// mustCallTool invokes name with args, decodes the JSON body into out,
// and fails the test on any transport, tool, or JSON error.
func mustCallTool(t *testing.T, c *mcpclient.Client, name string, args map[string]any, out any) {
	t.Helper()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = name
	if args != nil {
		req.Params.Arguments = args
	}
	result, err := c.CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("%s: CallTool: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("%s: tool error: %+v", name, result.Content)
	}
	text := extractText(result)
	if text == "" {
		t.Fatalf("%s: empty text content", name)
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		t.Fatalf("%s: decode result: %v\nbody: %s", name, err, text)
	}
}

func extractText(r *mcpgo.CallToolResult) string {
	for _, c := range r.Content {
		if tc, ok := c.(mcpgo.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
