package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"

	"github.com/spf13/cobra"
)

var modelCmd = &cobra.Command{
	Use:   "model",
	Short: "Model operations",
}

// writeScenariosFlag controls whether `mgtt model validate` regenerates
// the scenarios.yaml sidecar for the model (and the workspace
// scenarios.index.yaml when no explicit path was given).
var writeScenariosFlag bool

var modelValidateCmd = &cobra.Command{
	Use:   "validate [path]",
	Short: "Validate system.model.yaml",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// If no path was supplied AND --write-scenarios was asked for,
		// treat this as a workspace walk: find every model.yaml under
		// CWD, regenerate each, and write the workspace index.
		explicitPath := len(args) > 0
		if writeScenariosFlag && !explicitPath {
			return runWorkspaceWriteScenarios(cmd)
		}

		path := "system.model.yaml"
		if explicitPath {
			path = args[0]
		}
		return runSingleModelValidate(cmd, path, writeScenariosFlag)
	},
}

// runSingleModelValidate validates one model file at path. When
// writeScenarios is true, regenerate scenarios.yaml beside it. On every
// call, if a scenarios.yaml already exists, compare its source_hash
// against the current content — error if they differ.
func runSingleModelValidate(cmd *cobra.Command, path string, writeScenarios bool) error {
	m, err := model.Load(path)
	if err != nil {
		return err
	}

	reg := providersupport.LoadAllForUse()

	// Warn on legacy bare-name provider refs before running validation.
	for _, entry := range m.Meta.Providers {
		ref, parseErr := model.ParseProviderRef(entry)
		if parseErr != nil {
			continue // malformed refs are caught by model.Validate
		}
		if ref.LegacyBareName {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"⚠ model uses bare provider name %q; consider %q\n",
				ref.Name, "<namespace>/"+ref.Name+"@<version>")
		}
	}

	result := model.Validate(m, reg)

	// Build depCounts: map component name → number of direct dependencies.
	// Use -1 to signal "no deps but has a healthy override".
	depCounts := make(map[string]int, len(m.Components))
	for _, name := range m.Order {
		comp := m.Components[name]
		count := 0
		for _, dep := range comp.Depends {
			count += len(dep.On)
		}
		if count == 0 && len(comp.HealthyRaw) > 0 {
			depCounts[name] = -1
		} else {
			depCounts[name] = count
		}
	}

	renderModelValidate(cmd.OutOrStdout(), result, m.Order, depCounts)

	if result.HasErrors() {
		os.Exit(1)
	}

	// Drift detection: if scenarios.yaml exists next to the model, its
	// stored source_hash must still match the current model + types.
	scenariosPath := filepath.Join(filepath.Dir(path), "scenarios.yaml")
	if _, err := os.Stat(scenariosPath); err == nil {
		f, err := os.Open(scenariosPath)
		if err != nil {
			return err
		}
		_, committedHash, err := scenarios.Read(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("read %s: %w", scenariosPath, err)
		}
		typePaths := collectTypePaths(reg)
		currentHash, err := scenarios.ComputeSourceHash(path, typePaths)
		if err != nil {
			return err
		}
		if committedHash != currentHash {
			return fmt.Errorf("%s is stale: source_hash=%s but current content hashes to %s. Run `mgtt model validate --write-scenarios` and commit.", scenariosPath, committedHash, currentHash)
		}
	}

	if writeScenarios {
		if _, err := regenerateScenariosFor(cmd, m, reg, path); err != nil {
			return err
		}
	}
	return nil
}

// regenerateScenariosFor enumerates scenarios for m and writes
// scenarios.yaml beside modelPath. Returns the IndexEntry that would go
// into scenarios.index.yaml when called from a workspace walk.
func regenerateScenariosFor(cmd *cobra.Command, m *model.Model, reg *providersupport.Registry, modelPath string) (scenarios.IndexEntry, error) {
	scs := scenarios.Enumerate(m, reg)
	typePaths := collectTypePaths(reg)
	hash, err := scenarios.ComputeSourceHash(modelPath, typePaths)
	if err != nil {
		return scenarios.IndexEntry{}, fmt.Errorf("compute source hash: %w", err)
	}
	outPath := filepath.Join(filepath.Dir(modelPath), "scenarios.yaml")
	f, err := os.Create(outPath)
	if err != nil {
		return scenarios.IndexEntry{}, fmt.Errorf("create %s: %w", outPath, err)
	}
	if err := scenarios.Write(f, hash, scs); err != nil {
		f.Close()
		return scenarios.IndexEntry{}, fmt.Errorf("write scenarios.yaml: %w", err)
	}
	if err := f.Close(); err != nil {
		return scenarios.IndexEntry{}, err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %d scenarios to %s\n", len(scs), outPath)
	return scenarios.IndexEntry{
		Name:          m.Meta.Name,
		ModelPath:     modelPath,
		ScenariosPath: outPath,
		Hash:          hash,
		Count:         len(scs),
	}, nil
}

// runWorkspaceWriteScenarios walks CWD for every model.yaml, regenerates
// each, detects meta.name collisions, and writes scenarios.index.yaml at
// the workspace root.
func runWorkspaceWriteScenarios(cmd *cobra.Command) error {
	modelPaths, err := findWorkspaceModels(".")
	if err != nil {
		return err
	}
	if len(modelPaths) == 0 {
		return fmt.Errorf("no model.yaml files found under current directory")
	}

	reg := providersupport.LoadAllForUse()
	entries := make([]scenarios.IndexEntry, 0, len(modelPaths))
	for _, mp := range modelPaths {
		m, err := model.Load(mp)
		if err != nil {
			return fmt.Errorf("load %s: %w", mp, err)
		}
		result := model.Validate(m, reg)
		if result.HasErrors() {
			return fmt.Errorf("%s: model has validation errors; run `mgtt model validate %s` first", mp, mp)
		}
		entry, err := regenerateScenariosFor(cmd, m, reg, mp)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
	}

	seen := map[string]string{}
	for _, e := range entries {
		if prev, ok := seen[e.Name]; ok {
			return fmt.Errorf("two models with meta.name=%q: %s and %s", e.Name, prev, e.ModelPath)
		}
		seen[e.Name] = e.ModelPath
	}

	f, err := os.Create("scenarios.index.yaml")
	if err != nil {
		return fmt.Errorf("create scenarios.index.yaml: %w", err)
	}
	defer f.Close()
	if err := scenarios.WriteIndex(f, entries); err != nil {
		return fmt.Errorf("write scenarios.index.yaml: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "wrote workspace index with %d model(s) to scenarios.index.yaml\n", len(entries))
	return nil
}

// findWorkspaceModels walks root looking for files literally named
// model.yaml, skipping vendor/build directories.
func findWorkspaceModels(root string) ([]string, error) {
	skip := map[string]bool{
		".git":         true,
		".mgtt":        true,
		"node_modules": true,
		"site":         true,
	}
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if info.IsDir() {
			if skip[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if name == "model.yaml" {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// collectTypePaths returns the on-disk paths of every type YAML the
// registry knows about, de-duplicated. Types whose SourcePath is empty
// (inline types, tests) are skipped — callers that need byte-level
// coverage for those cases should re-hash provider manifests directly.
func collectTypePaths(reg *providersupport.Registry) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range reg.All() {
		for _, t := range p.Types {
			if t.SourcePath == "" {
				continue
			}
			if seen[t.SourcePath] {
				continue
			}
			seen[t.SourcePath] = true
			out = append(out, t.SourcePath)
		}
	}
	return out
}

func init() {
	modelValidateCmd.Flags().BoolVar(&writeScenariosFlag, "write-scenarios", false,
		"regenerate scenarios.yaml (and scenarios.index.yaml when no explicit model path is given) at the model's directory")
	modelCmd.AddCommand(modelValidateCmd)
	rootCmd.AddCommand(modelCmd)
}

// renderModelValidate writes a human-readable validation report to w.
//
// components is the ordered list of component names to display.
// depCounts maps component name → number of direct dependencies.
func renderModelValidate(w io.Writer, result *model.ValidationResult, components []string, depCounts map[string]int) {
	// Index errors by component name for quick lookup.
	compErrors := make(map[string][]model.ValidationError)
	var globalErrors []model.ValidationError
	for _, e := range result.Errors {
		if e.Component == "" {
			globalErrors = append(globalErrors, e)
		} else {
			compErrors[e.Component] = append(compErrors[e.Component], e)
		}
	}

	// Determine the longest component name for alignment.
	maxLen := 0
	for _, name := range components {
		if len(name) > maxLen {
			maxLen = len(name)
		}
	}

	// Print one line per component.
	for _, name := range components {
		errs := compErrors[name]
		if len(errs) == 0 {
			// Component is valid — pick the right description.
			desc := componentDesc(name, depCounts[name])
			fmt.Fprintf(w, "  %s %-*s  %s\n", checkmark(true), maxLen, name, desc)
		} else {
			// One line per error for this component.
			for i, e := range errs {
				if i == 0 {
					fmt.Fprintf(w, "  %s %-*s  %s\n", checkmark(false), maxLen, name, e.Message)
				} else {
					fmt.Fprintf(w, "  %s %-*s  %s\n", checkmark(false), maxLen, "", e.Message)
				}
				if e.Suggestion != "" {
					indent := strings.Repeat(" ", 2+1+1+maxLen+2) // "  ✗ " + padding + "  "
					fmt.Fprintf(w, "%sdid you mean %q?\n", indent, e.Suggestion)
				}
			}
		}
	}

	// Print global errors (cycles etc.) — not tied to a component.
	for _, e := range globalErrors {
		fmt.Fprintf(w, "  %s %s\n", checkmark(false), e.Message)
	}

	// Blank line before summary.
	fmt.Fprintln(w)

	errCount := len(result.Errors)
	warnCount := len(result.Warnings)
	fmt.Fprintf(w, "  %s · %s · %s\n",
		pluralize(len(components), "component", "components"),
		pluralize(errCount, "error", "errors"),
		pluralize(warnCount, "warning", "warnings"),
	)
}

// componentDesc returns the per-component status description.
//
// Convention for depCount:
//   - -1 : no dependencies, but component has a healthy-override (HealthyRaw)
//   - 0  : no dependencies, no healthy-override
//   - N  : N direct dependencies
func componentDesc(_ string, depCount int) string {
	if depCount < 0 {
		return "healthy override valid"
	}
	if depCount == 0 {
		return "no dependencies"
	}
	return pluralize(depCount, "dependency valid", "dependencies valid")
}
