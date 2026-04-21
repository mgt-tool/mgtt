// Package mcp exposes mgtt's constraint engine as an MCP service. See
// docs/superpowers/specs/2026-04-22-llm-support-design.md for the full
// contract. The handler reuses the CLI's engine path — engine.Plan,
// scenarios, facts, incident — via thin tool wrappers.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

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
// transport closes (stdin EOF for stdio, SIGINT for HTTP).
func Run(cfg Config) error {
	h := NewHandler(cfg)
	s := server.NewMCPServer("mgtt", cfg.Version)

	registerAbout(s, h)

	if cfg.HTTP {
		return runHTTP(s, cfg)
	}
	return server.ServeStdio(s)
}

func registerAbout(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("about",
		mcpgo.WithDescription("Server metadata — version, active transports, current safety posture"),
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

func runHTTP(_ *server.MCPServer, _ Config) error {
	return fmt.Errorf("http transport not yet implemented") // Task 3
}
