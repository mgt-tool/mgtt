package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
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
	providerInstallImage    string
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
--registry overrides it per-invocation.

Image install:
  --image <ref>              Pull a provider image and register it locally.
                             The ref MUST include a @sha256: digest.
                             An optional positional arg overrides the install name.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		w := cmd.OutOrStdout()

		// --image takes full priority; skip all git/local/registry resolution.
		if providerInstallImage != "" {
			var nameHint string
			if len(args) > 0 {
				nameHint = args[0]
			}
			return installFromImage(cmd.Context(), w, providerInstallImage, nameHint, providersupport.NewDockerCmd())
		}

		// Without --image, the existing resolution chain requires at least one name.
		if len(args) == 0 {
			return fmt.Errorf("requires at least 1 arg(s), only received 0")
		}
		for _, name := range args {
			if err := installProvider(w, name); err != nil {
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
	providerInstallCmd.Flags().StringVar(&providerInstallImage, "image", "",
		"install provider from a Docker image ref (must include @sha256: digest); optional positional arg overrides install name")
	providerCmd.AddCommand(providerInstallCmd)
	rootCmd.AddCommand(providerCmd)
}

// installFromImage pulls a provider image, extracts /manifest.yaml from it,
// and registers the provider locally without cloning any git source.
// The ref must include a @sha256: digest (enforced by ValidateImageRef).
// nameHint, if non-empty, overrides the name from the manifest.
// docker is the DockerCmd to use; callers pass providersupport.NewDockerCmd() in production.
func installFromImage(ctx context.Context, w io.Writer, ref, nameHint string, docker *providersupport.DockerCmd) error {
	if err := providersupport.ValidateImageRef(ref); err != nil {
		return err
	}

	fmt.Fprintf(w, "→ pulling %s\n", ref)
	if err := docker.PullImage(ctx, ref); err != nil {
		return err
	}

	fmt.Fprintf(w, "→ extracting /manifest.yaml\n")
	manifestBytes, err := docker.ExtractManifest(ctx, ref)
	if err != nil {
		return err
	}

	p, err := providersupport.LoadFromBytes(manifestBytes)
	if err != nil {
		return fmt.Errorf("parse manifest.yaml from image: %w", err)
	}
	if p.Meta.Name == "" {
		return fmt.Errorf("manifest.yaml from image is missing meta.name")
	}

	name := nameHint
	if name == "" {
		name = p.Meta.Name
	}

	// Install-time capability validation. Per the image-runner capabilities
	// spec, unknown caps must fail install loudly — silent drop at probe time
	// turns a typo in manifest.yaml into a cryptic "probe didn't reach X"
	// debugging session. Shell-fallback refusal is handled by validate.Static.
	if len(p.Needs) > 0 {
		var unknown []string
		for _, n := range p.Needs {
			if !probe.Known(n) {
				unknown = append(unknown, n)
			}
		}
		if len(unknown) > 0 {
			return fmt.Errorf(
				"provider declares unknown image capabilities: %s (known: %s); add them to $MGTT_HOME/capabilities.yaml or fix manifest.yaml",
				strings.Join(unknown, ", "), strings.Join(probe.KnownNames(), ", "))
		}
		fmt.Fprintf(w, "→ capabilities: %s\n", strings.Join(p.Needs, ", "))
	}
	if p.Network != "" && p.Network != "bridge" {
		fmt.Fprintf(w, "→ network: %s\n", p.Network)
	}
	if !p.ReadOnly {
		fmt.Fprintf(w, "⚠ writes: %s\n", strings.TrimSpace(p.WritesNote))
	}

	providersRoot, err := providersupport.InstallRoot()
	if err != nil {
		return fmt.Errorf("get install root: %w", err)
	}
	destDir := filepath.Join(providersRoot, name)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create provider dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "manifest.yaml"), manifestBytes, 0o644); err != nil {
		return fmt.Errorf("write manifest.yaml: %w", err)
	}

	// Multi-file providers (kubernetes, tempo, quickwit, terraform) keep type
	// definitions in /types/<name>.yaml. Extract them too when present — a
	// missing /types/ just means inline types, which is fine.
	typeFiles, err := docker.ExtractTypes(ctx, ref)
	if err != nil {
		return fmt.Errorf("extract /types: %w", err)
	}
	if len(typeFiles) > 0 {
		typesDir := filepath.Join(destDir, "types")
		if err := os.MkdirAll(typesDir, 0o755); err != nil {
			return fmt.Errorf("create types dir: %w", err)
		}
		for name, body := range typeFiles {
			if err := os.WriteFile(filepath.Join(typesDir, name), body, 0o644); err != nil {
				return fmt.Errorf("write type %s: %w", name, err)
			}
		}
	}

	meta := providersupport.InstallMeta{
		Method:      providersupport.InstallMethodImage,
		Namespace:   deriveNamespace(ref),
		Source:      ref,
		InstalledAt: time.Now().UTC(),
		Version:     p.Meta.Version,
	}
	if err := providersupport.WriteInstallMeta(destDir, meta); err != nil {
		return err
	}

	fmt.Fprintf(w, "✓ installed %s %s (image)\n", name, p.Meta.Version)
	return nil
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
			if _, err := os.Stat(filepath.Join(nameOrPath, "manifest.yaml")); err == nil {
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

	// Load manifest.yaml to get canonical name.
	p, err := providersupport.LoadFromFile(filepath.Join(srcDir, "manifest.yaml"))
	if err != nil {
		return fmt.Errorf("load manifest.yaml: %w", err)
	}

	// Copy to the canonical install root (honoring MGTT_HOME).
	providersRoot, err := providersupport.InstallRoot()
	if err != nil {
		return fmt.Errorf("get install root: %w", err)
	}
	destDir := filepath.Join(providersRoot, p.Meta.Name)
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

	// Write install metadata so `mgtt provider list` and resolver can use it.
	gitMeta := providersupport.InstallMeta{
		Method:      providersupport.InstallMethodGit,
		Namespace:   deriveNamespace(nameOrPath),
		Source:      nameOrPath,
		InstalledAt: time.Now().UTC(),
		Version:     p.Meta.Version,
	}
	// Non-fatal: list and resolve degrade gracefully when the file is absent.
	if err := providersupport.WriteInstallMeta(destDir, gitMeta); err != nil {
		fmt.Fprintf(w, "⚠ could not write install metadata: %v\n", err)
	}

	posture := "read-only"
	if !p.ReadOnly {
		posture = "writes"
	}
	fmt.Fprintf(w, "  %s %-12s  v%s  %s\n",
		checkmark(true), p.Meta.Name, p.Meta.Version, posture)
	return nil
}

// deriveNamespace extracts the namespace (org/user) from a git URL or image
// reference. For git URLs like "https://github.com/mgt-tool/mgtt-provider-tempo"
// or image refs like "ghcr.io/mgt-tool/mgtt-provider-tempo:0.2.0@sha256:...",
// the first path segment after the host is the namespace.
// Returns "" if the namespace cannot be determined.
func deriveNamespace(urlOrRef string) string {
	// Strip common git/image prefixes to get the path component.
	var path string
	for _, prefix := range []string{
		"https://", "http://", "git://", "git@",
	} {
		if strings.HasPrefix(urlOrRef, prefix) {
			// After stripping prefix: host/path... — find next slash.
			rest := strings.TrimPrefix(urlOrRef, prefix)
			// For git@: host:path — replace colon with slash.
			rest = strings.Replace(rest, ":", "/", 1)
			if idx := strings.Index(rest, "/"); idx >= 0 {
				path = rest[idx+1:]
			}
			break
		}
	}

	// For image refs (no http/git prefix): strip digest first, then host/path.
	if path == "" {
		// Strip @sha256:... digest if present.
		ref := urlOrRef
		if idx := strings.Index(ref, "@"); idx >= 0 {
			ref = ref[:idx]
		}
		// Strip tag.
		if idx := strings.LastIndex(ref, ":"); idx >= 0 {
			ref = ref[:idx]
		}
		// Now ref is something like "ghcr.io/mgt-tool/mgtt-provider-tempo".
		// The host is the first segment; path follows.
		if idx := strings.Index(ref, "/"); idx >= 0 {
			path = ref[idx+1:]
		}
	}

	if path == "" {
		return ""
	}

	// The first segment of the path is the namespace.
	segments := strings.SplitN(path, "/", 2)
	if len(segments) < 2 || segments[0] == "" {
		// Only one segment — this is a bare name, no namespace.
		return ""
	}
	return segments[0]
}

// cloneRepo clones a git repo to a temp dir and returns the path.
// Expects manifest.yaml at the repo root.
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
	if _, err := os.Stat(filepath.Join(tmpDir, "manifest.yaml")); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("cloned repo has no manifest.yaml")
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
