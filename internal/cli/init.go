// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const initTemplate = `meta:
  name: my-system
  version: "1.0"
  providers:
    - kubernetes

components:
  # Add your infrastructure components here.
  # Example:
  #
  # frontend:
  #   type: deployment
  #   depends:
  #     - on: api
  #
  # api:
  #   type: deployment
  #   depends:
  #     - on: db
  #
  # db:
  #   type: rds_instance
  #   healthy:
  #     - connection_count < 500
`

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Scaffold a blank system.model.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "system.model.yaml"
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists", path)
			}
			if err := os.WriteFile(path, []byte(initTemplate), 0o644); err != nil {
				return fmt.Errorf("init: write %s: %w", path, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ created %s  — edit to describe your system\n", path)
			return nil
		},
	}
}

func init() {
	registerCommand(newInitCmd)
}
