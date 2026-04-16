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

var providerUninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Uninstall a provider (runs optional uninstall hook, then removes the directory)",
	Long: `Removes an installed provider from ~/.mgtt/providers/<name>/.

If the provider declares hooks.uninstall in provider.yaml, that script is
executed before the directory is removed (same environment as the install hook:
MGTT_PROVIDER_DIR and MGTT_PROVIDER_NAME are set).

This command intentionally does NOT check meta.requires.mgtt — you must
always be able to remove a provider you can no longer use.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return uninstallProvider(cmd.OutOrStdout(), args[0])
	},
}

func init() {
	providerCmd.AddCommand(providerUninstallCmd)
}

func uninstallProvider(w io.Writer, name string) error {
	dir := providersupport.ProviderDir(name)
	if dir == "" {
		return fmt.Errorf("provider %q is not installed", name)
	}

	// Load install metadata to determine how the provider was installed.
	meta, err := providersupport.ReadInstallMeta(dir)
	if err != nil {
		// Metadata read failed, but still try to uninstall. Warn the operator.
		fmt.Fprintf(w, "  warning: could not read install metadata: %v\n", err)
		// Treat it as a git install (pre-metadata backward-compatible default)
		// and continue uninstall.
	}

	// Load provider.yaml to discover the uninstall hook. This uses the
	// un-gated LoadEmbedded (not LoadForUse) because uninstall must work
	// even when the provider is version-incompatible with the running mgtt.
	p, err := providersupport.LoadEmbedded(name)
	if err != nil {
		// Provider dir exists but YAML is unparseable — still remove it.
		// The operator chose to uninstall; honour that.
		fmt.Fprintf(w, "  warning: could not load provider.yaml: %v\n", err)
		fmt.Fprintf(w, "  removing %s\n", dir)
		return os.RemoveAll(dir)
	}

	// Run uninstall hook if declared, but only for git-installed providers.
	// Image-installed providers don't have hooks on disk — skip the hook entirely.
	if p.Hooks.Uninstall != "" && meta.Method != providersupport.InstallMethodImage {
		hookPath := filepath.Join(dir, p.Hooks.Uninstall)
		if _, err := os.Stat(hookPath); err == nil {
			fmt.Fprintf(w, "  running uninstall hook: %s\n", hookPath)
			hookCmd := exec.Command("bash", hookPath)
			hookCmd.Dir = dir
			hookCmd.Env = append(os.Environ(),
				"MGTT_PROVIDER_DIR="+dir,
				"MGTT_PROVIDER_NAME="+name,
			)
			hookCmd.Stdout = w
			hookCmd.Stderr = w
			if err := hookCmd.Run(); err != nil {
				fmt.Fprintf(w, "  warning: uninstall hook failed: %v (removing directory anyway)\n", err)
			}
		}
	}

	fmt.Fprintf(w, "  removing %s\n", dir)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove provider directory: %w", err)
	}

	// For image-installed providers, inform the user about the Docker image cache.
	if meta.Method == providersupport.InstallMethodImage && meta.Source != "" {
		fmt.Fprintf(w, "  ℹ image %s remains in your local Docker cache; remove with:\n", meta.Source)
		fmt.Fprintf(w, "    docker rmi %s\n", meta.Source)
	}

	fmt.Fprintf(w, "  %s uninstalled %s\n", checkmark(true), name)
	return nil
}
