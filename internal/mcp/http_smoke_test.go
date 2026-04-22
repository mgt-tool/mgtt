package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TestHTTP_SmokeDiagnosisLoopWithBearerAuth stands up the real streamable
// HTTP handler (same path `runHTTP` would wire) behind the real bearer
// middleware, points mcp-go's HTTP client at it with a valid token, and
// drives a full diagnosis loop. Catches transport bugs that the
// in-process test can't — envelope framing, header handling, URL routing,
// auth integration.
func TestHTTP_SmokeDiagnosisLoopWithBearerAuth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MGTT_HOME", dir)
	modelPath := writeMinimalModel(t, dir)

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	const token = "smoke-test-token"
	mcpSrv := buildServer(Config{Version: "smoke"})
	streamable := server.NewStreamableHTTPServer(mcpSrv)
	httpSrv := httptest.NewServer(withBearerAuth(token, streamable))
	t.Cleanup(httpSrv.Close)

	client, err := mcpclient.NewStreamableHttpClient(
		httpSrv.URL,
		transport.WithHTTPHeaders(map[string]string{
			"Authorization": "Bearer " + token,
		}),
	)
	if err != nil {
		t.Fatalf("NewStreamableHttpClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client.Start: %v", err)
	}
	if _, err := client.Initialize(ctx, mcpgo.InitializeRequest{}); err != nil {
		t.Fatalf("client.Initialize: %v", err)
	}

	// Full loop, same shape the in-process test runs — but over real HTTP.
	var about AboutResult
	mustCallTool(t, client, "about", nil, &about)
	if about.Version != "smoke" {
		t.Errorf("about.version: got %q want smoke", about.Version)
	}

	var start IncidentStartResult
	mustCallTool(t, client, "incident.start", map[string]any{
		"model_ref": modelPath,
		"id":        "smoke-inc",
	}, &start)

	var add FactAddResult
	mustCallTool(t, client, "fact.add", map[string]any{
		"incident_id": start.IncidentID,
		"component":   "api",
		"key":         "operator_says_healthy",
		"value":       true,
	}, &add)
	if !add.Appended {
		t.Error("fact.add over HTTP should report appended: true")
	}

	var snap IncidentSnapshotResult
	mustCallTool(t, client, "incident.snapshot", map[string]any{
		"incident_id": start.IncidentID,
	}, &snap)
	if len(snap.Facts) != 1 {
		t.Errorf("snapshot.facts: got %d want 1", len(snap.Facts))
	}

	var end IncidentEndResult
	mustCallTool(t, client, "incident.end", map[string]any{
		"incident_id": start.IncidentID,
		"verdict":     "smoke ok",
	}, &end)
	if !end.Saved {
		t.Error("incident.end over HTTP should report saved: true")
	}
}

// TestHTTP_SmokeMissingBearerFails401 asserts the transport-layer auth
// gate is wired — a request without the expected header must fail before
// the MCP dispatcher sees it.
func TestHTTP_SmokeMissingBearerFails401(t *testing.T) {
	mcpSrv := buildServer(Config{})
	streamable := server.NewStreamableHTTPServer(mcpSrv)
	httpSrv := httptest.NewServer(withBearerAuth("real-token", streamable))
	t.Cleanup(httpSrv.Close)

	// Raw POST — bypasses the mcp-go client so we test exactly the wire
	// behaviour a misconfigured agent would see.
	req, err := http.NewRequest(http.MethodPost, httpSrv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d want 401", resp.StatusCode)
	}
}

// TestHTTP_SmokeWrongBearerFails401 — wrong token must be rejected even
// when the scheme is correct. Guards against any accidental bypass.
func TestHTTP_SmokeWrongBearerFails401(t *testing.T) {
	mcpSrv := buildServer(Config{})
	streamable := server.NewStreamableHTTPServer(mcpSrv)
	httpSrv := httptest.NewServer(withBearerAuth("real-token", streamable))
	t.Cleanup(httpSrv.Close)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req, err := http.NewRequest(http.MethodPost, httpSrv.URL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer wrong-one")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d want 401", resp.StatusCode)
	}
}
