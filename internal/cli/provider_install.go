package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"mgtt/internal/providersupport"

	"github.com/spf13/cobra"
)

var providerCmd = &cobra.Command{
	Use:   "provider",
	Short: "Provider operations",
}

var providerInstallCmd = &cobra.Command{
	Use:   "install [names...]",
	Short: "Install one or more providers",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, name := range args {
			if err := installProvider(cmd.OutOrStdout(), name); err != nil {
				return fmt.Errorf("provider %q: %w", name, err)
			}
		}
		return nil
	},
}

func init() {
	providerCmd.AddCommand(providerInstallCmd)
	rootCmd.AddCommand(providerCmd)
}

// installProvider installs a single provider by name.
// For v0, providers are in-repo only (no URL cloning).
// Steps:
//  1. Find source provider directory (local providers/<name>/)
//  2. Copy it to ~/.mgtt/providers/<name>/
//  3. Run hooks/install.sh if declared
//  4. Load and validate provider.yaml
//  5. Render summary
func installProvider(w io.Writer, name string) error {
	// 1. Determine source directory.
	srcDir := ""
	if home := os.Getenv("MGTT_HOME"); home != "" {
		candidate := filepath.Join(home, "providers", name)
		if _, err := os.Stat(filepath.Join(candidate, "provider.yaml")); err == nil {
			srcDir = candidate
		}
	}
	if srcDir == "" {
		// Fall back to local providers directory relative to CWD.
		candidate := filepath.Join("providers", name)
		if _, err := os.Stat(filepath.Join(candidate, "provider.yaml")); err == nil {
			srcDir = candidate
		}
	}
	if srcDir == "" {
		return fmt.Errorf("provider directory not found")
	}

	// 2. Determine destination: ~/.mgtt/providers/<name>/
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	destDir := filepath.Join(homeDir, ".mgtt", "providers", name)
	if err := copyDir(srcDir, destDir); err != nil {
		return fmt.Errorf("copy provider directory: %w", err)
	}
	fmt.Fprintf(w, "  copied %s -> %s\n", srcDir, destDir)

	// 3. Load provider.yaml to read hooks.
	p, err := providersupport.LoadFromFile(filepath.Join(destDir, "provider.yaml"))
	if err != nil {
		return fmt.Errorf("load provider.yaml: %w", err)
	}

	// 4. Run install hook if declared.
	if p.Hooks.Install != "" {
		hookPath := filepath.Join(destDir, p.Hooks.Install)
		fmt.Fprintf(w, "  running install hook: %s\n", hookPath)
		hookCmd := exec.Command("bash", hookPath)
		hookCmd.Dir = destDir
		hookCmd.Stdout = w
		hookCmd.Stderr = w
		if err := hookCmd.Run(); err != nil {
			return fmt.Errorf("install hook failed: %w", err)
		}
	}

	// 5. Render summary.
	fmt.Fprintf(w, "  %s %-12s  v%s  auth: %s  access: %s\n",
		checkmark(true),
		p.Meta.Name,
		p.Meta.Version,
		p.Auth.Strategy,
		p.Auth.Access.Probes,
	)
	return nil
}

// copyDir recursively copies src to dst. dst is created if it does not exist.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		// Copy file.
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
