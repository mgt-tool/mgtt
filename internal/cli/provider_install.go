package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

// installProvider installs a provider by name or path.
// If the argument contains a path separator or starts with ".", it's treated
// as a directory path. Otherwise it's looked up by name in $MGTT_HOME/providers/
// or ./providers/.
//
// Steps:
//  1. Find source provider directory
//  2. Copy it to ~/.mgtt/providers/<name>/
//  3. Run hooks/install.sh if declared
//  4. Load and validate provider.yaml
//  5. Render summary
func installProvider(w io.Writer, nameOrPath string) error {
	// 1. Determine source directory.
	srcDir := ""

	// Check if argument is a path (contains separator or starts with .)
	if filepath.IsAbs(nameOrPath) || strings.HasPrefix(nameOrPath, ".") || strings.Contains(nameOrPath, string(filepath.Separator)) {
		candidate := nameOrPath
		if _, err := os.Stat(filepath.Join(candidate, "provider.yaml")); err == nil {
			srcDir = candidate
		}
	}

	if srcDir == "" {
		// Look up by name
		name := nameOrPath
		if home := os.Getenv("MGTT_HOME"); home != "" {
			candidate := filepath.Join(home, "providers", name)
			if _, err := os.Stat(filepath.Join(candidate, "provider.yaml")); err == nil {
				srcDir = candidate
			}
		}
		if srcDir == "" {
			candidate := filepath.Join("providers", name)
			if _, err := os.Stat(filepath.Join(candidate, "provider.yaml")); err == nil {
				srcDir = candidate
			}
		}
	}

	if srcDir == "" {
		return fmt.Errorf("provider directory not found (tried path and name lookup)")
	}

	// 2. Load provider.yaml first to get the canonical name.
	p, err := providersupport.LoadFromFile(filepath.Join(srcDir, "provider.yaml"))
	if err != nil {
		return fmt.Errorf("load provider.yaml: %w", err)
	}
	providerName := p.Meta.Name

	// 3. Determine destination: ~/.mgtt/providers/<name>/
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	destDir := filepath.Join(homeDir, ".mgtt", "providers", providerName)
	if err := copyDir(srcDir, destDir); err != nil {
		return fmt.Errorf("copy provider directory: %w", err)
	}
	fmt.Fprintf(w, "  copied %s -> %s\n", srcDir, destDir)

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
