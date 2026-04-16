package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/registry"

	"github.com/spf13/cobra"
)

var providerCmd = &cobra.Command{
	Use:   "provider",
	Short: "Provider operations",
}

var (
	providerInstallNoCache  bool
	providerInstallRegistry string
)

var providerInstallCmd = &cobra.Command{
	Use:   "install [names...]",
	Short: "Install one or more providers",
	Long: `Install providers by name (resolved via registry), git URL, or local path.

Registry resolution:
  --registry <url>           Override the registry URL for this invocation.
  --registry disabled        Skip registry resolution entirely (air-gapped).
  --registry file://<path>   Load the registry from a local file (mirrored).

The MGTT_REGISTRY_URL env var sets the same value persistently;
--registry overrides it per-invocation.`,
	Args: cobra.MinimumNArgs(1),
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
	providerInstallCmd.Flags().BoolVar(&providerInstallNoCache, "no-cache", false, "bypass registry cache")
	providerInstallCmd.Flags().StringVar(&providerInstallRegistry, "registry", "",
		"registry URL override (use 'disabled' / 'none' / 'off' to skip registry resolution; or 'file://<path>' for local index)")
	providerCmd.AddCommand(providerInstallCmd)
	rootCmd.AddCommand(providerCmd)
}

// installProvider installs a provider by name, path, or git URL.
//
// Resolution order:
//  1. Git URL → clone directly
//  2. Local path → use directly
//  3. Local name lookup → SearchDirs()
//  4. Registry fetch → HTTPS index → clone the URL
func installProvider(w io.Writer, nameOrPath string) error {
	srcDir := ""
	var tmpDirs []string
	defer func() {
		for _, d := range tmpDirs {
			os.RemoveAll(d)
		}
	}()

	// 1. Git URL
	if isGitURL(nameOrPath) {
		dir, err := cloneRepo(w, nameOrPath)
		if err != nil {
			return err
		}
		tmpDirs = append(tmpDirs, dir)
		srcDir = dir
	}

	// 2. Local path
	if srcDir == "" {
		if filepath.IsAbs(nameOrPath) || strings.HasPrefix(nameOrPath, ".") || strings.Contains(nameOrPath, string(filepath.Separator)) {
			if _, err := os.Stat(filepath.Join(nameOrPath, "provider.yaml")); err == nil {
				srcDir = nameOrPath
			}
		}
	}

	// 3. Local name lookup
	if srcDir == "" {
		if dir := providersupport.ProviderDir(nameOrPath); dir != "" {
			srcDir = dir
		}
	}

	// 4. Registry lookup. Skipped silently when the operator opted out
	// (MGTT_REGISTRY_URL=disabled or --registry disabled). Other registry
	// errors print a warning and fall through.
	registryDisabled := false
	if srcDir == "" {
		reg, err := registry.Fetch(registry.Source{
			URL:     providerInstallRegistry,
			NoCache: providerInstallNoCache,
		})
		switch {
		case errors.Is(err, registry.ErrRegistryDisabled):
			registryDisabled = true
		case err != nil:
			fmt.Fprintf(w, "  warning: could not fetch registry: %v\n", err)
		default:
			if entry, ok := reg.Lookup(nameOrPath); ok {
				dir, err := cloneRepo(w, entry.URL)
				if err != nil {
					return err
				}
				tmpDirs = append(tmpDirs, dir)
				srcDir = dir
			}
		}
	}

	if srcDir == "" {
		if registryDisabled {
			// Wrap the sentinel so callers can errors.Is(err, registry.ErrRegistryDisabled)
			// to distinguish opt-out from network failure.
			return fmt.Errorf("%w: %q is not a git URL or local path — pass one explicitly",
				registry.ErrRegistryDisabled, nameOrPath)
		}
		return fmt.Errorf("not found (tried git URL, local path, name lookup, and registry)")
	}

	// Load provider.yaml to get canonical name.
	p, err := providersupport.LoadFromFile(filepath.Join(srcDir, "provider.yaml"))
	if err != nil {
		return fmt.Errorf("load provider.yaml: %w", err)
	}

	// Copy to ~/.mgtt/providers/<name>/
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	destDir := filepath.Join(homeDir, ".mgtt", "providers", p.Meta.Name)
	if err := copyDir(srcDir, destDir); err != nil {
		return fmt.Errorf("copy provider: %w", err)
	}

	// Run install hook if declared.
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

	fmt.Fprintf(w, "  %s %-12s  v%s  auth: %s  access: %s\n",
		checkmark(true), p.Meta.Name, p.Meta.Version, p.Auth.Strategy, p.Auth.Access.Probes)
	return nil
}

// cloneRepo clones a git repo to a temp dir and returns the path.
// Expects provider.yaml at the repo root.
func cloneRepo(w io.Writer, url string) (string, error) {
	fmt.Fprintf(w, "  cloning %s...\n", url)
	tmpDir, err := os.MkdirTemp("", "mgtt-provider-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	cmd := exec.Command("git", "clone", "--depth=1", url, tmpDir)
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("git clone: %w", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "provider.yaml")); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("cloned repo has no provider.yaml")
	}
	return tmpDir, nil
}

// copyDir recursively copies src to dst.
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
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
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
