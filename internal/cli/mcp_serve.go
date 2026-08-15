// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"github.com/mgt-tool/mgtt/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	cfg := mcp.Config{}
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP server (stdio by default, HTTP with --http)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.Version = version
			return mcp.Run(cfg)
		},
	}
	serve.Flags().BoolVar(&cfg.HTTP, "http", false, "run streamable HTTP transport instead of stdio")
	serve.Flags().StringVar(&cfg.Listen, "listen", ":8080", "listen address for HTTP mode")
	serve.Flags().StringVar(&cfg.TokenEnv, "token-env", "", "env var name holding the bearer token (required in HTTP mode)")
	serve.Flags().BoolVar(&cfg.ReadonlyOnly, "readonly-only", false, "reject probes whose provider does not declare read_only: true")
	serve.Flags().StringVar(&cfg.OnWrite, "on-write", "run", "behavior when a write probe is next: pause | run | fail")
	serve.Flags().IntVar(&cfg.MaxExecutePerIncident, "max-execute-per-incident", 50, "rate limit for executed probes per incident")
	serve.Flags().IntVar(&cfg.ProbeTimeoutSeconds, "probe-timeout", 30, "per-probe timeout in seconds (max 300)")

	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server for LLM agents",
	}
	mcpCmd.AddCommand(serve)
	return mcpCmd
}

func init() {
	registerCommand(newMCPCmd)
}
