// Package mcp exposes mgtt's constraint engine as an MCP service. See
// docs/superpowers/specs/2026-04-22-llm-support-design.md for the full
// contract. The handler reuses the CLI's engine path — engine.Plan,
// scenarios, facts, incident — via thin tool wrappers.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Config holds all runtime knobs for the MCP server.
type Config struct {
	Version               string // populated by the CLI from cli.version at startup
	HTTP                  bool
	Listen                string
	TokenEnv              string
	ReadonlyOnly          bool
	OnWrite               string // "pause" | "run" | "fail"
	MaxExecutePerIncident int
	ProbeTimeoutSeconds   int
}

// Run boots the MCP server with the given config. Blocks until the
// transport closes (stdin EOF for stdio, SIGINT/SIGTERM for HTTP).
func Run(cfg Config) error {
	s := buildServer(cfg)
	if cfg.HTTP {
		return runHTTP(s, cfg)
	}
	return server.ServeStdio(s)
}

// buildServer wires the handler and registers every tool. Exposed as a
// package-internal helper so the in-process client tests can drive the
// exact same server Run would expose on a transport.
func buildServer(cfg Config) *server.MCPServer {
	h := NewHandler(cfg)
	s := server.NewMCPServer("mgtt", cfg.Version)

	registerAbout(s, h)
	registerIncidentStart(s, h)
	registerIncidentEnd(s, h)
	registerFactAdd(s, h)
	registerFactsList(s, h)
	registerPlan(s, h)
	registerProbe(s, h)
	registerScenariosList(s, h)
	registerScenariosAlive(s, h)
	registerIncidentSnapshot(s, h)

	return s
}

// rawOutput wraps a schema constant as a json.RawMessage for
// mcpgo.WithRawOutputSchema. Wire-side agents that call tools/list see
// the returned shape, not an auto-derived approximation.
func rawOutput(s string) mcpgo.ToolOption {
	return mcpgo.WithRawOutputSchema(json.RawMessage(s))
}

func registerAbout(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("about",
		mcpgo.WithDescription("Server metadata — version, active transports, current safety posture"),
		rawOutput(AboutOutputSchema),
	)
	s.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		result, err := h.About()
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("about: marshal result: %w", err)
		}
		return mcpgo.NewToolResultText(string(body)), nil
	})
}

func registerIncidentStart(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("incident.start",
		mcpgo.WithDescription("Create a new incident bound to a model. Returns incident_id for subsequent tool calls."),
		mcpgo.WithString("model_ref",
			mcpgo.Required(),
			mcpgo.Description("path to system.model.yaml the server can read"),
		),
		mcpgo.WithString("id",
			mcpgo.Description("optional incident id; generated if omitted"),
		),
		mcpgo.WithArray("suspect",
			mcpgo.Description(`optional component.state hints, e.g. ["api.crash_looping"]`),
		),
		rawOutput(IncidentStartOutputSchema),
	)
	s.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		var p IncidentStartParams
		p.ModelRef = req.GetString("model_ref", "")
		p.ID = req.GetString("id", "")
		if raw := req.GetStringSlice("suspect", nil); raw != nil {
			p.Suspect = raw
		}
		result, err := h.IncidentStart(p)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("incident.start: marshal result: %w", err)
		}
		return mcpgo.NewToolResultText(string(body)), nil
	})
}

func registerIncidentEnd(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("incident.end",
		mcpgo.WithDescription("Close an incident. Persists the end timestamp and optional verdict; returns saved=true on success."),
		mcpgo.WithString("incident_id",
			mcpgo.Required(),
			mcpgo.Description("id returned by incident.start"),
		),
		mcpgo.WithString("verdict",
			mcpgo.Description("optional human or agent note recording the conclusion"),
		),
		rawOutput(IncidentEndOutputSchema),
	)
	s.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		p := IncidentEndParams{
			IncidentID: req.GetString("incident_id", ""),
			Verdict:    req.GetString("verdict", ""),
		}
		result, err := h.IncidentEnd(p)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("incident.end: marshal result: %w", err)
		}
		return mcpgo.NewToolResultText(string(body)), nil
	})
}

func registerFactAdd(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("fact.add",
		mcpgo.WithDescription("Append an observation to an incident's fact store."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		mcpgo.WithString("component", mcpgo.Required()),
		mcpgo.WithString("key", mcpgo.Required()),
		// value intentionally untyped — any JSON primitive or string is fine.
		mcpgo.WithString("note", mcpgo.Description("optional note on provenance")),
		rawOutput(FactAddOutputSchema),
	)
	s.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		p := FactAddParams{
			IncidentID: stringArg(args, "incident_id"),
			Component:  stringArg(args, "component"),
			Key:        stringArg(args, "key"),
			Value:      args["value"],
			Note:       stringArg(args, "note"),
		}
		result, err := h.FactAdd(p)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("fact.add: marshal result: %w", err)
		}
		return mcpgo.NewToolResultText(string(body)), nil
	})
}

func registerFactsList(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("facts.list",
		mcpgo.WithDescription("List facts recorded for an incident, optionally filtered to one component."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		mcpgo.WithString("component", mcpgo.Description("optional component filter")),
		rawOutput(FactsListOutputSchema),
	)
	s.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		p := FactsListParams{
			IncidentID: req.GetString("incident_id", ""),
			Component:  req.GetString("component", ""),
		}
		result, err := h.FactsList(p)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("facts.list: marshal result: %w", err)
		}
		return mcpgo.NewToolResultText(string(body)), nil
	})
}

func registerPlan(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("plan",
		mcpgo.WithDescription("Compute the current path tree for an incident and suggest the next probe. Does not execute anything."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		mcpgo.WithString("component", mcpgo.Description("optional entry point override")),
		rawOutput(PlanOutputSchema),
	)
	s.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		p := PlanParams{
			IncidentID: req.GetString("incident_id", ""),
			Component:  req.GetString("component", ""),
		}
		result, err := h.Plan(p)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("plan: marshal result: %w", err)
		}
		return mcpgo.NewToolResultText(string(body)), nil
	})
}

func registerProbe(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("probe",
		mcpgo.WithDescription("Render or execute the engine's next suggested probe. execute=false is read-only (rendered command, no system touch); execute=true runs it and appends the resulting fact."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		mcpgo.WithString("component", mcpgo.Description("optional entry-point override")),
		mcpgo.WithBoolean("execute", mcpgo.Required(),
			mcpgo.Description("when false the probe is rendered but not run")),
		rawOutput(ProbeOutputSchema),
	)
	s.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		p := ProbeParams{
			IncidentID: req.GetString("incident_id", ""),
			Component:  req.GetString("component", ""),
			Execute:    req.GetBool("execute", false),
		}
		result, err := h.Probe(p)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("probe: marshal result: %w", err)
		}
		return mcpgo.NewToolResultText(string(body)), nil
	})
}

func registerScenariosList(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("scenarios.list",
		mcpgo.WithDescription("Enumerate every failure chain the engine considers for this incident's model."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		rawOutput(ScenariosListOutputSchema),
	)
	s.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		result, err := h.ScenariosList(ScenariosListParams{IncidentID: req.GetString("incident_id", "")})
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("scenarios.list: marshal result: %w", err)
		}
		return mcpgo.NewToolResultText(string(body)), nil
	})
}

func registerScenariosAlive(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("scenarios.alive",
		mcpgo.WithDescription("Subset of enumerated scenarios still consistent with the incident's facts."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		rawOutput(ScenariosListOutputSchema),
	)
	s.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		result, err := h.ScenariosAlive(ScenariosAliveParams{IncidentID: req.GetString("incident_id", "")})
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("scenarios.alive: marshal result: %w", err)
		}
		return mcpgo.NewToolResultText(string(body)), nil
	})
}

func registerIncidentSnapshot(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("incident.snapshot",
		mcpgo.WithDescription("Export an incident's full diagnostic memory — surviving and eliminated scenarios, facts, current suggestion, status."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		rawOutput(IncidentSnapshotOutputSchema),
	)
	s.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		result, err := h.IncidentSnapshot(IncidentSnapshotParams{IncidentID: req.GetString("incident_id", "")})
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("incident.snapshot: marshal result: %w", err)
		}
		return mcpgo.NewToolResultText(string(body)), nil
	})
}

// stringArg extracts a string from the raw arguments map. Missing or
// non-string values return "" — handler methods validate required fields.
func stringArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// runHTTP serves the MCP endpoint until SIGINT / SIGTERM, then gracefully
// drains in-flight requests for up to shutdownGrace before returning.
func runHTTP(s *server.MCPServer, cfg Config) error {
	token, err := resolveToken(cfg)
	if err != nil {
		return err
	}
	streamable := server.NewStreamableHTTPServer(s)
	authed := withBearerAuth(token, streamable)
	addr := cfg.Listen
	if addr == "" {
		addr = ":8080"
	}
	httpSrv := &http.Server{Addr: addr, Handler: authed}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.ListenAndServe() }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
		return nil
	}
}

// shutdownGrace bounds how long runHTTP waits for in-flight requests
// to drain after SIGINT/SIGTERM. Probes have a 30s default timeout,
// so 35s covers one in-flight probe plus envelope teardown.
const shutdownGrace = 35 * time.Second
