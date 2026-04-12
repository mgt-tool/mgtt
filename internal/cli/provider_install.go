package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sajonaro/mgtt/internal/providersupport"

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

// installProvider installs a provider by name, path, or git URL.
//
// Resolution order:
//  1. Git URL (https:// or git@) → clone to temp dir → install from there
//  2. Local path (starts with . or / or contains separator) → use directly
//  3. Name lookup → $MGTT_HOME/providers/<name>/ or ./providers/<name>/
//
// Steps after resolution:
//  1. Load provider.yaml to get canonical name
//  2. Copy to ~/.mgtt/providers/<name>/
//  3. Run hooks/install.sh if declared
//  4. Render summary
func installProvider(w io.Writer, nameOrPath string) error {
	srcDir := ""

	// Git URL: clone to temp dir
	if isGitURL(nameOrPath) {
		fmt.Fprintf(w, "  cloning %s...\n", nameOrPath)
		tmpDir, err := os.MkdirTemp("", "mgtt-provider-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		cloneCmd := exec.Command("git", "clone", "--depth=1", nameOrPath, tmpDir)
		cloneCmd.Stderr = w
		if err := cloneCmd.Run(); err != nil {
			return fmt.Errorf("git clone: %w", err)
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "provider.yaml")); err != nil {
			return fmt.Errorf("cloned repo has no provider.yaml")
		}
		srcDir = tmpDir
	}

	// Local path
	if srcDir == "" {
		if filepath.IsAbs(nameOrPath) || strings.HasPrefix(nameOrPath, ".") || strings.Contains(nameOrPath, string(filepath.Separator)) {
			if _, err := os.Stat(filepath.Join(nameOrPath, "provider.yaml")); err == nil {
				srcDir = nameOrPath
			}
		}
	}

	// Name lookup
	if srcDir == "" {
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
		return fmt.Errorf("provider %q not found (tried git URL, local path, and name lookup)", nameOrPath)
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

		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

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

// isGitURL returns true if the string looks like a git-cloneable URL.
func isGitURL(s string) bool {
	return strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "git@") ||
		strings.HasPrefix(s, "git://")
}
