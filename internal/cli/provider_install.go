// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
	"github.com/mgt-tool/mgtt/internal/registry"

	"github.com/spf13/cobra"
)

// installOpts carries the per-invocation flag state for provider
// install. Scoped to the Cobra command constructor; tests pass an
// explicit value rather than mutating package globals.
type installOpts struct {
	NoCache  bool
	Registry string // registry URL override
	Image    string // --image <ref>
}

func newProviderInstallCmd() *cobra.Command {
	opts := &installOpts{}
	cmd := &cobra.Command{
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
			if opts.Image != "" {
				var nameHint string
				if len(args) > 0 {
					nameHint = args[0]
				}
				return installFromImage(cmd.Context(), w, opts.Image, nameHint, providersupport.NewDockerCmd())
			}
			if len(args) == 0 {
				return fmt.Errorf("requires at least 1 arg(s), only received 0")
			}
			for _, name := range args {
				if err := installProvider(w, name, *opts); err != nil {
					return fmt.Errorf("provider %q: %w", name, err)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.NoCache, "no-cache", false, "bypass registry cache")
	cmd.Flags().StringVar(&opts.Registry, "registry", "",
		"registry URL override (use 'disabled' / 'none' / 'off' to skip registry resolution; or 'file://<path>' for local index)")
	cmd.Flags().StringVar(&opts.Image, "image", "",
		"install provider from a Docker image ref (must include @sha256: digest); optional positional arg overrides install name")
	return cmd
}

func newProviderCmd() *cobra.Command {
	providerCmd := &cobra.Command{
		Use:   "provider",
		Short: "Provider operations",
	}
	providerCmd.AddCommand(newProviderInstallCmd())
	providerCmd.AddCommand(newProviderLsCmd())
	providerCmd.AddCommand(newProviderInspectCmd())
	providerCmd.AddCommand(newProviderUninstallCmd())
	providerCmd.AddCommand(newProviderValidateCmd())
	return providerCmd
}

func init() {
	registerCommand(newProviderCmd)
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
	p, err := parseImageManifest(manifestBytes)
	if err != nil {
		return err
	}

	name := nameHint
	if name == "" {
		name = p.Meta.Name
	}
	if err := validateImageCaps(w, p); err != nil {
		return err
	}
	if p.Runtime.NetworkMode != "" && p.Runtime.NetworkMode != "bridge" {
		fmt.Fprintf(w, "→ network: %s\n", p.Runtime.NetworkMode)
	}
	if !p.ReadOnly {
		fmt.Fprintf(w, "⚠ writes: %s\n", strings.TrimSpace(p.WritesNote))
	}

	destDir, err := writeImageInstall(ctx, name, manifestBytes, ref, docker)
	if err != nil {
		return err
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

// parseImageManifest decodes a manifest.yaml pulled out of a provider
// image and applies the image-install sanity checks (non-empty name,
// not the reserved generic name, has install.image recipe).
func parseImageManifest(manifestBytes []byte) (*providersupport.Provider, error) {
	p, err := providersupport.LoadFromBytes(manifestBytes)
	if err != nil {
		return nil, fmt.Errorf("parse manifest.yaml from image: %w", err)
	}
	if p.Meta.Name == "" {
		return nil, fmt.Errorf("manifest.yaml from image is missing meta.name")
	}
	if providersupport.IsGenericName(p.Meta.Name) {
		return nil, fmt.Errorf("provider meta.name %q is reserved for mgtt's built-in generic fallback — rename the provider before installing", p.Meta.Name)
	}
	if p.Install.Image == nil {
		return nil, fmt.Errorf("provider %q has no image-install recipe (manifest declares install.source only); install without --image to build from source", p.Meta.Name)
	}
	return p, nil
}

// validateImageCaps enforces the install-time capability contract:
// every name in runtime.needs must resolve against the known vocabulary
// (built-ins + operator overrides). A typo in manifest.yaml caught at
// probe time surfaces as a cryptic "probe didn't reach X" — catch it
// at install time instead.
func validateImageCaps(w io.Writer, p *providersupport.Provider) error {
	if len(p.Runtime.Needs) == 0 {
		return nil
	}
	needNames := slices.Sorted(maps.Keys(p.Runtime.Needs))
	var unknown []string
	for _, n := range needNames {
		if !probe.Known(n) {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf(
			"provider declares unknown image capabilities: %s (known: %s); add them to $MGTT_HOME/capabilities.yaml or fix manifest.yaml",
			strings.Join(unknown, ", "), strings.Join(probe.KnownNames(), ", "))
	}
	fmt.Fprintf(w, "→ capabilities: %s\n", strings.Join(needNames, ", "))
	return nil
}

// writeImageInstall lays down the provider directory: manifest.yaml at
// the root, optional types/ sub-directory populated from ExtractTypes.
// Returns the on-disk install dir for the caller to write install-meta
// into.
func writeImageInstall(ctx context.Context, name string, manifestBytes []byte, ref string, docker *providersupport.DockerCmd) (string, error) {
	providersRoot, err := providersupport.InstallRoot()
	if err != nil {
		return "", fmt.Errorf("get install root: %w", err)
	}
	destDir := filepath.Join(providersRoot, name)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create provider dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "manifest.yaml"), manifestBytes, 0o644); err != nil {
		return "", fmt.Errorf("write manifest.yaml: %w", err)
	}
	// Multi-file providers (kubernetes, tempo, quickwit, terraform) keep
	// types under /types/<name>.yaml. Missing /types is fine (inline-types
	// providers).
	typeFiles, err := docker.ExtractTypes(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("extract /types: %w", err)
	}
	if len(typeFiles) > 0 {
		typesDir := filepath.Join(destDir, "types")
		if err := os.MkdirAll(typesDir, 0o755); err != nil {
			return "", fmt.Errorf("create types dir: %w", err)
		}
		for name, body := range typeFiles {
			if err := os.WriteFile(filepath.Join(typesDir, name), body, 0o644); err != nil {
				return "", fmt.Errorf("write type %s: %w", name, err)
			}
		}
	}
	return destDir, nil
}

// installProvider installs a provider by name, path, or git URL.
//
// Resolution order:
//  1. Git URL → clone directly
//  2. Local path → use directly
//  3. Local name lookup → SearchDirs()
//  4. Registry fetch → HTTPS index → clone the URL
func installProvider(w io.Writer, nameOrPath string, opts installOpts) error {
	var tmpDirs []string
	defer func() {
		for _, d := range tmpDirs {
			os.RemoveAll(d)
		}
	}()
	srcDir, err := resolveInstallSource(w, nameOrPath, opts, &tmpDirs)
	if err != nil {
		return err
	}
	p, err := loadSourceManifest(srcDir)
	if err != nil {
		return err
	}
	destDir, err := copyToInstallRoot(srcDir, p.Meta.Name)
	if err != nil {
		return err
	}
	if err := runSourceBuildHook(w, p, destDir); err != nil {
		return err
	}
	writeSourceInstallMeta(w, destDir, nameOrPath, p)
	renderInstallSuccess(w, p)
	return nil
}

// loadSourceManifest reads and validates manifest.yaml for a source
// install: rejects the reserved "generic" name and requires an
// install.source recipe (non-image caller).
func loadSourceManifest(srcDir string) (*providersupport.Provider, error) {
	p, err := providersupport.LoadFromFile(filepath.Join(srcDir, "manifest.yaml"))
	if err != nil {
		return nil, fmt.Errorf("load manifest.yaml: %w", err)
	}
	if providersupport.IsGenericName(p.Meta.Name) {
		return nil, fmt.Errorf("provider meta.name %q is reserved for mgtt's built-in generic fallback — rename the provider before installing", p.Meta.Name)
	}
	if p.Install.Source == nil {
		return nil, fmt.Errorf("provider %q has no source-install recipe (manifest declares install.image only); pass --image <ref> to install from the published image", p.Meta.Name)
	}
	return p, nil
}

// copyToInstallRoot copies srcDir into $MGTT_HOME/providers/<name> and
// returns the destination directory.
func copyToInstallRoot(srcDir, name string) (string, error) {
	root, err := providersupport.InstallRoot()
	if err != nil {
		return "", fmt.Errorf("get install root: %w", err)
	}
	destDir := filepath.Join(root, name)
	if err := copyDir(srcDir, destDir); err != nil {
		return "", fmt.Errorf("copy provider: %w", err)
	}
	return destDir, nil
}

// runSourceBuildHook runs the declared install/build script (relative
// path under destDir) with the same environment semantics as uninstall.
// No-op when the provider declares no build hook.
func runSourceBuildHook(w io.Writer, p *providersupport.Provider, destDir string) error {
	if p.Install.Source == nil || p.Install.Source.Build == "" {
		return nil
	}
	hookPath := filepath.Join(destDir, p.Install.Source.Build)
	fmt.Fprintf(w, "  running install hook: %s\n", hookPath)
	cmd := exec.Command("bash", hookPath)
	cmd.Dir = destDir
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install hook failed: %w", err)
	}
	return nil
}

// writeSourceInstallMeta writes .mgtt-install.json with the git-install
// provenance. Metadata is non-fatal: list and resolve degrade gracefully
// when the file is absent (pre-metadata legacy installs).
func writeSourceInstallMeta(w io.Writer, destDir, nameOrPath string, p *providersupport.Provider) {
	meta := providersupport.InstallMeta{
		Method:      providersupport.InstallMethodGit,
		Namespace:   deriveNamespace(nameOrPath),
		Source:      nameOrPath,
		InstalledAt: time.Now().UTC(),
		Version:     p.Meta.Version,
	}
	if err := providersupport.WriteInstallMeta(destDir, meta); err != nil {
		fmt.Fprintf(w, "⚠ could not write install metadata: %v\n", err)
	}
}

// renderInstallSuccess prints the one-line install confirmation with
// name / version / posture.
func renderInstallSuccess(w io.Writer, p *providersupport.Provider) {
	posture := "read-only"
	if !p.ReadOnly {
		posture = "writes"
	}
	fmt.Fprintf(w, "  %s %-12s  v%s  %s\n",
		checkmark(true), p.Meta.Name, p.Meta.Version, posture)
}

// resolveInstallSource walks the four-stage resolution order
// (git URL → local path → local name → registry fetch) and returns the
// source directory to install from. tmpDirs is appended to whenever a
// temp clone is made so the caller can clean up.
func resolveInstallSource(w io.Writer, nameOrPath string, opts installOpts, tmpDirs *[]string) (string, error) {
	if isGitURL(nameOrPath) {
		dir, err := cloneRepo(w, nameOrPath)
		if err != nil {
			return "", err
		}
		*tmpDirs = append(*tmpDirs, dir)
		return dir, nil
	}
	if looksLikePath(nameOrPath) {
		if _, err := os.Stat(filepath.Join(nameOrPath, "manifest.yaml")); err == nil {
			return nameOrPath, nil
		}
	}
	if dir := providersupport.ProviderDir(nameOrPath); dir != "" {
		return dir, nil
	}
	// Registry lookup — skipped silently when operator opted out
	// (MGTT_REGISTRY_URL=disabled or --registry disabled).
	reg, err := registry.Fetch(registry.Source{
		URL:     opts.Registry,
		NoCache: opts.NoCache,
	})
	if errors.Is(err, registry.ErrRegistryDisabled) {
		return "", fmt.Errorf("%w: %q is not a git URL or local path — pass one explicitly",
			registry.ErrRegistryDisabled, nameOrPath)
	}
	if err != nil {
		fmt.Fprintf(w, "  warning: could not fetch registry: %v\n", err)
		return "", fmt.Errorf("not found (tried git URL, local path, name lookup, and registry)")
	}
	entry, ok := reg.Lookup(nameOrPath)
	if !ok {
		return "", fmt.Errorf("not found (tried git URL, local path, name lookup, and registry)")
	}
	dir, err := cloneRepo(w, entry.URL)
	if err != nil {
		return "", err
	}
	*tmpDirs = append(*tmpDirs, dir)
	return dir, nil
}

// looksLikePath reports whether nameOrPath reads as a local filesystem
// target (absolute, dot-prefixed, or contains a separator) rather than a
// bare provider name.
func looksLikePath(nameOrPath string) bool {
	return filepath.IsAbs(nameOrPath) ||
		strings.HasPrefix(nameOrPath, ".") ||
		strings.Contains(nameOrPath, string(filepath.Separator))
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
