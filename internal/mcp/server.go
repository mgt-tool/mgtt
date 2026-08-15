// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

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

// dispatch adapts a typed handler method into mcpgo's tool-handler shape.
// bind extracts typed params from the raw request; fn executes the method
// and returns a JSON-marshalable result. toolName appears only in the
// marshal-failure error message so operators can trace which tool tripped.
func dispatch[P any, R any](toolName string, bind func(mcpgo.CallToolRequest) P, fn func(P) (R, error)) server.ToolHandlerFunc {
	return func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		result, err := fn(bind(req))
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("%s: marshal result: %w", toolName, err)
		}
		return mcpgo.NewToolResultText(string(body)), nil
	}
}

// incidentIDOnly is the common binder for tools whose only input is
// `incident_id`. Generic over the concrete Params struct type; fn pulls
// the string from req and stuffs it into a zero-valued P via setter.
func incidentIDOnly[P any](set func(*P, string)) func(mcpgo.CallToolRequest) P {
	return func(req mcpgo.CallToolRequest) P {
		var p P
		set(&p, req.GetString("incident_id", ""))
		return p
	}
}

func registerAbout(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("about",
		mcpgo.WithDescription("Server metadata — version, active transports, current safety posture"),
		rawOutput(AboutOutputSchema),
	)
	s.AddTool(tool, dispatch("about",
		func(mcpgo.CallToolRequest) struct{} { return struct{}{} },
		func(struct{}) (*AboutResult, error) { return h.About() },
	))
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
	s.AddTool(tool, dispatch("incident.start",
		func(req mcpgo.CallToolRequest) IncidentStartParams {
			return IncidentStartParams{
				ModelRef: req.GetString("model_ref", ""),
				ID:       req.GetString("id", ""),
				Suspect:  req.GetStringSlice("suspect", nil),
			}
		},
		h.IncidentStart,
	))
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
	s.AddTool(tool, dispatch("incident.end",
		func(req mcpgo.CallToolRequest) IncidentEndParams {
			return IncidentEndParams{
				IncidentID: req.GetString("incident_id", ""),
				Verdict:    req.GetString("verdict", ""),
			}
		},
		h.IncidentEnd,
	))
}

func registerFactAdd(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("fact.add",
		mcpgo.WithDescription("Append an observation to an incident's fact store."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		mcpgo.WithString("component", mcpgo.Required()),
		mcpgo.WithString("key", mcpgo.Required()),
		mcpgo.WithAny("value", mcpgo.Description("observed value — any JSON primitive, object, or array")),
		mcpgo.WithString("note", mcpgo.Description("optional note on provenance")),
		rawOutput(FactAddOutputSchema),
	)
	s.AddTool(tool, dispatch("fact.add",
		func(req mcpgo.CallToolRequest) FactAddParams {
			return FactAddParams{
				IncidentID: req.GetString("incident_id", ""),
				Component:  req.GetString("component", ""),
				Key:        req.GetString("key", ""),
				Value:      req.GetArguments()["value"], // WithAny — no typed getter
				Note:       req.GetString("note", ""),
			}
		},
		h.FactAdd,
	))
}

func registerFactsList(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("facts.list",
		mcpgo.WithDescription("List facts recorded for an incident, optionally filtered to one component."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		mcpgo.WithString("component", mcpgo.Description("optional component filter")),
		rawOutput(FactsListOutputSchema),
	)
	s.AddTool(tool, dispatch("facts.list",
		func(req mcpgo.CallToolRequest) FactsListParams {
			return FactsListParams{
				IncidentID: req.GetString("incident_id", ""),
				Component:  req.GetString("component", ""),
			}
		},
		h.FactsList,
	))
}

func registerPlan(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("plan",
		mcpgo.WithDescription("Compute the current path tree for an incident and suggest the next probe. Does not execute anything."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		mcpgo.WithString("component", mcpgo.Description("optional entry point override")),
		rawOutput(PlanOutputSchema),
	)
	s.AddTool(tool, dispatch("plan",
		func(req mcpgo.CallToolRequest) PlanParams {
			return PlanParams{
				IncidentID: req.GetString("incident_id", ""),
				Component:  req.GetString("component", ""),
			}
		},
		h.Plan,
	))
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
	s.AddTool(tool, dispatch("probe",
		func(req mcpgo.CallToolRequest) ProbeParams {
			return ProbeParams{
				IncidentID: req.GetString("incident_id", ""),
				Component:  req.GetString("component", ""),
				Execute:    req.GetBool("execute", false),
			}
		},
		h.Probe,
	))
}

func registerScenariosList(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("scenarios.list",
		mcpgo.WithDescription("Enumerate every failure chain the engine considers for this incident's model."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		rawOutput(ScenariosListOutputSchema),
	)
	s.AddTool(tool, dispatch("scenarios.list",
		incidentIDOnly(func(p *ScenariosListParams, id string) { p.IncidentID = id }),
		h.ScenariosList,
	))
}

func registerScenariosAlive(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("scenarios.alive",
		mcpgo.WithDescription("Subset of enumerated scenarios still consistent with the incident's facts."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		rawOutput(ScenariosListOutputSchema),
	)
	s.AddTool(tool, dispatch("scenarios.alive",
		incidentIDOnly(func(p *ScenariosAliveParams, id string) { p.IncidentID = id }),
		h.ScenariosAlive,
	))
}

func registerIncidentSnapshot(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("incident.snapshot",
		mcpgo.WithDescription("Export an incident's full diagnostic memory — surviving and eliminated scenarios, facts, current suggestion, status."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		rawOutput(IncidentSnapshotOutputSchema),
	)
	s.AddTool(tool, dispatch("incident.snapshot",
		incidentIDOnly(func(p *IncidentSnapshotParams, id string) { p.IncidentID = id }),
		h.IncidentSnapshot,
	))
}

// runHTTP serves the MCP endpoint until SIGINT / SIGTERM, then gracefully
// drains in-flight requests for up to shutdownGrace before returning.
func runHTTP(s *server.MCPServer, cfg Config) error {
	token, err := resolveToken(cfg)
	if err != nil {
		return err
	}
	httpSrv := buildHTTPServer(s, cfg, token)
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
		return gracefulShutdown(httpSrv, cfg, serveErr)
	}
}

// buildHTTPServer composes the bearer-auth-wrapped streamable handler
// with a hardened http.Server (slow-loris protection via
// ReadHeaderTimeout, header-bomb protection via MaxHeaderBytes, idle
// budget so keep-alive goroutines don't pin).
func buildHTTPServer(s *server.MCPServer, cfg Config, token string) *http.Server {
	addr := cfg.Listen
	if addr == "" {
		addr = ":8080"
	}
	streamable := server.NewStreamableHTTPServer(s)
	return &http.Server{
		Addr:              addr,
		Handler:           withBearerAuth(token, streamable),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // streaming can exceed probe timeout; ctx handles it
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 14,
	}
}

// gracefulShutdown runs httpSrv.Shutdown under the configured drain
// budget, then waits for the serve goroutine to actually return so the
// listener is truly gone when runHTTP exits.
func gracefulShutdown(httpSrv *http.Server, cfg Config, serveErr <-chan error) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace(cfg))
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http serve goroutine: %w", err)
	}
	return nil
}

// shutdownGrace bounds how long runHTTP waits for in-flight requests to
// drain after SIGINT/SIGTERM. One in-flight probe's timeout plus a 5s
// envelope; capped to 5 minutes so a misconfigured probe_timeout does not
// hang SIGTERM indefinitely.
func shutdownGrace(cfg Config) time.Duration {
	probe := cfg.ProbeTimeoutSeconds
	if probe <= 0 {
		probe = 30
	}
	if probe > probeTimeoutSecondsMax {
		probe = probeTimeoutSecondsMax
	}
	return time.Duration(probe+5) * time.Second
}
