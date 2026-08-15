// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mgt-tool/mgtt/internal/providersupport"

	"github.com/spf13/cobra"
)

func newProviderUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <name>",
		Short: "Uninstall a provider (runs optional uninstall hook, then removes the directory)",
		Long: `Removes an installed provider from ~/.mgtt/providers/<name>/.

If the provider declares install.source.clean in manifest.yaml, that script is
executed before the directory is removed (same environment as the install hook:
MGTT_PROVIDER_DIR and MGTT_PROVIDER_NAME are set).

This command intentionally does NOT check meta.requires.mgtt — you must
always be able to remove a provider you can no longer use.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstallProvider(cmd.OutOrStdout(), args[0])
		},
	}
}

func uninstallProvider(w io.Writer, name string) error {
	dir := providersupport.ProviderDir(name)
	if dir == "" {
		return fmt.Errorf("provider %q is not installed", name)
	}
	// ReadInstallMeta error is non-fatal — pre-metadata legacy installs
	// and corrupt metadata should not block uninstall.
	meta, err := providersupport.ReadInstallMeta(dir)
	if err != nil {
		fmt.Fprintf(w, "  warning: could not read install metadata: %v\n", err)
	}
	// LoadEmbedded (un-gated) lets uninstall work even when the
	// provider is version-incompatible with the running mgtt.
	p, err := providersupport.LoadEmbedded(name)
	if err != nil {
		fmt.Fprintf(w, "  warning: could not load manifest.yaml: %v\n", err)
		fmt.Fprintf(w, "  removing %s\n", dir)
		return os.RemoveAll(dir)
	}
	runUninstallHookIfAny(w, dir, name, p, meta)
	fmt.Fprintf(w, "  removing %s\n", dir)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove provider directory: %w", err)
	}
	reportImageCacheHint(w, meta)
	fmt.Fprintf(w, "  %s uninstalled %s\n", checkmark(true), name)
	return nil
}

// runUninstallHookIfAny executes install.source.clean when the provider
// declares one AND was source-installed (image installs have no hook
// script on disk). Hook failures are warned but don't block removal.
func runUninstallHookIfAny(w io.Writer, dir, name string, p *providersupport.Provider, meta providersupport.InstallMeta) {
	if p.Install.Source == nil || p.Install.Source.Clean == "" {
		return
	}
	if meta.Method == providersupport.InstallMethodImage {
		return
	}
	hookPath := filepath.Join(dir, p.Install.Source.Clean)
	if _, err := os.Stat(hookPath); err != nil {
		return
	}
	fmt.Fprintf(w, "  running uninstall hook: %s\n", hookPath)
	cmd := exec.Command("bash", hookPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"MGTT_PROVIDER_DIR="+dir,
		"MGTT_PROVIDER_NAME="+name,
	)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(w, "  warning: uninstall hook failed: %v (removing directory anyway)\n", err)
	}
}

// reportImageCacheHint reminds the operator that `docker rmi` is the
// way to reclaim disk for an image-installed provider; no-op for
// git-installed or pre-metadata providers.
func reportImageCacheHint(w io.Writer, meta providersupport.InstallMeta) {
	if meta.Method != providersupport.InstallMethodImage || meta.Source == "" {
		return
	}
	fmt.Fprintf(w, "  ℹ image %s remains in your local Docker cache; remove with:\n", meta.Source)
	fmt.Fprintf(w, "    docker rmi %s\n", meta.Source)
}
