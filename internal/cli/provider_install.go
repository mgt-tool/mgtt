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

// builtinRegistry maps provider names to their git URL and subdirectory path.
// Providers that live in the main mgtt repo use a subdir; standalone repos leave path empty.
var builtinRegistry = map[string]struct {
	url  string
	path string
}{
	"kubernetes": {url: "https://github.com/sajonaro/mgtt", path: "providers/kubernetes"},
	"aws":        {url: "https://github.com/sajonaro/mgtt", path: "providers/aws"},
}

// installProvider installs a provider by name, path, or git URL.
//
// Resolution order:
//  1. Git URL (https:// or git@) → clone to temp dir → install from there
//  2. Local path (starts with . or / or contains separator) → use directly
//  3. Name lookup → $MGTT_HOME/providers/<name>/ or ./providers/<name>/
//  4. Built-in registry → known providers cloned from git
//
// Steps after resolution:
//  1. Load provider.yaml to get canonical name
//  2. Copy to ~/.mgtt/providers/<name>/
//  3. Run hooks/install.sh if declared
//  4. Render summary
func installProvider(w io.Writer, nameOrPath string) error {
	srcDir := ""
	var tmpDirs []string
	defer func() {
		for _, d := range tmpDirs {
			os.RemoveAll(d)
		}
	}()

	// Git URL: clone to temp dir
	if isGitURL(nameOrPath) {
		dir, err := cloneProvider(w, nameOrPath, "")
		if err != nil {
			return err
		}
		tmpDirs = append(tmpDirs, dir)
		srcDir = dir
	}

	// Local path
	if srcDir == "" {
		if filepath.IsAbs(nameOrPath) || strings.HasPrefix(nameOrPath, ".") || strings.Contains(nameOrPath, string(filepath.Separator)) {
			if _, err := os.Stat(filepath.Join(nameOrPath, "provider.yaml")); err == nil {
				srcDir = nameOrPath
			}
		}
	}

	// Name lookup (local)
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

	// Built-in registry lookup
	if srcDir == "" {
		if entry, ok := builtinRegistry[nameOrPath]; ok {
			dir, err := cloneProvider(w, entry.url, entry.path)
			if err != nil {
				return err
			}
			tmpDirs = append(tmpDirs, dir)
			srcDir = dir
		}
	}

	if srcDir == "" {
		return fmt.Errorf("provider %q not found (tried git URL, local path, name lookup, and built-in registry)", nameOrPath)
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

// cloneProvider clones a git repo and returns the path containing provider.yaml.
// If subdir is non-empty, the provider lives in a subdirectory of the repo.
// The caller is responsible for cleaning up the returned temp directory.
func cloneProvider(w io.Writer, url, subdir string) (string, error) {
	fmt.Fprintf(w, "  cloning %s...\n", url)
	tmpDir, err := os.MkdirTemp("", "mgtt-provider-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	cloneCmd := exec.Command("git", "clone", "--depth=1", url, tmpDir)
	cloneCmd.Stderr = w
	if err := cloneCmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("git clone: %w", err)
	}

	srcDir := tmpDir
	if subdir != "" {
		srcDir = filepath.Join(tmpDir, subdir)
	}

	if _, err := os.Stat(filepath.Join(srcDir, "provider.yaml")); err != nil {
		os.RemoveAll(tmpDir)
		if subdir != "" {
			return "", fmt.Errorf("cloned repo has no provider.yaml at %s", subdir)
		}
		return "", fmt.Errorf("cloned repo has no provider.yaml")
	}

	// Return tmpDir as the root to clean up, but srcDir is what the caller uses.
	// Since srcDir may be a subdir, we copy just that part to a new temp location
	// so the caller gets a clean directory with only the provider.
	if subdir != "" {
		provDir, err := os.MkdirTemp("", "mgtt-provider-sub-*")
		if err != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("create temp dir: %w", err)
		}
		if err := copyDir(srcDir, provDir); err != nil {
			os.RemoveAll(tmpDir)
			os.RemoveAll(provDir)
			return "", fmt.Errorf("copy provider subdir: %w", err)
		}
		os.RemoveAll(tmpDir)
		return provDir, nil
	}

	return tmpDir, nil
}

// isGitURL returns true if the string looks like a git-cloneable URL.
func isGitURL(s string) bool {
	return strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "git@") ||
		strings.HasPrefix(s, "git://")
}
