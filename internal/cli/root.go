// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version is the string shown by `mgtt version`. It carries the value
// baked in by the Makefile via ldflags (-X ...cli.version=$(VERSION)),
// which is what `make build` and the docker image do. When the binary
// is produced another way — `go build ./cmd/mgtt`, `go install
// github.com/mgt-tool/mgtt/cmd/mgtt@v0.2.0`, or `... @main` — the
// fallback in init() below populates it from the Go toolchain's
// recorded build info (module version or VCS revision).
var version = "dev"

// commandFactories holds the top-level command constructors registered
// at package load. Each returns a fresh *cobra.Command with its own
// flag state on every call — so RootCmd() can be invoked repeatedly
// (tests call it per-case) without cross-run flag persistence.
var commandFactories []func() *cobra.Command

// registerCommand adds a constructor to the root-build list. Called
// from each subcommand file's init() instead of mutating a shared
// rootCmd directly.
func registerCommand(factory func() *cobra.Command) {
	commandFactories = append(commandFactories, factory)
}

func init() {
	populateVersionFromBuildInfo()
	registerCommand(func() *cobra.Command {
		return &cobra.Command{
			Use:   "version",
			Short: "Print version",
			Run: func(cmd *cobra.Command, args []string) {
				cmd.Println("mgtt version " + version)
			},
		}
	})
}

// populateVersionFromBuildInfo is a no-op when ldflags already set
// version to a real value; otherwise it tries (a) the module version
// (works for `go install ...@<tag>` and for pseudo-versions like
// `v0.1.5-0.20260419…-38d55eb4649f` from `@main`), then (b) the VCS
// revision from local-build metadata (works for `go build` from a
// checkout). Falls through to leave version == "dev" when neither is
// available (e.g. built outside Go modules).
func populateVersionFromBuildInfo() {
	if version != "dev" {
		return // ldflags set a real value; trust it
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		version = v
		return
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 {
			version = "devel+" + s.Value[:12]
			return
		}
	}
}

func Execute() error {
	return RootCmd().Execute()
}

// RootCmd builds a fresh command tree on each call. Tests invoking
// the CLI repeatedly get per-invocation flag state without manual resets.
func RootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "mgtt",
		Short: "Model Guided Troubleshooting Tool",
	}
	for _, f := range commandFactories {
		root.AddCommand(f())
	}
	return root
}
