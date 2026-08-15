// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

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

func newModelCmd() *cobra.Command {
	modelCmd := &cobra.Command{
		Use:   "model",
		Short: "Model operations",
	}
	modelCmd.AddCommand(newModelValidateCmd())
	modelCmd.AddCommand(newModelBuildCmd())
	modelCmd.AddCommand(newModelExportCmd())
	return modelCmd
}

func newModelValidateCmd() *cobra.Command {
	// writeScenarios regenerates the scenarios.yaml sidecar (and the
	// workspace scenarios.index.yaml when no explicit path was given).
	// checkScenarios is the fast-path drift-only mode: skip structural /
	// type / dep-ref validation, run only the scenarios.yaml drift check.
	var writeScenarios, checkScenarios bool
	cmd := &cobra.Command{
		Use:          "validate [path]",
		Short:        "Validate system.model.yaml",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true, // validation errors are not usage errors
		RunE: func(cmd *cobra.Command, args []string) error {
			explicitPath := len(args) > 0
			if writeScenarios && !explicitPath {
				return runWorkspaceWriteScenarios(cmd)
			}
			path := "system.model.yaml"
			if explicitPath {
				path = args[0]
			}
			if checkScenarios {
				return runScenariosDriftOnly(cmd, path)
			}
			return runSingleModelValidate(cmd, path, writeScenarios)
		},
	}
	cmd.Flags().BoolVar(&writeScenarios, "write-scenarios", false,
		"regenerate scenarios.yaml (and scenarios.index.yaml when no explicit model path is given) at the model's directory")
	cmd.Flags().BoolVar(&checkScenarios, "check-scenarios", false,
		"run only the scenarios.yaml drift check; skip structural / type / dep-ref validation")
	return cmd
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
	reg, err := loadRegistryForUse()
	if err != nil {
		return err
	}
	warnLegacyProviderRefs(cmd.ErrOrStderr(), m)

	result := model.Validate(m, reg)
	if !m.Meta.StrictTypes {
		for _, gf := range model.CollectGenericFallbacks(m, reg) {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"INFO: component %q uses generic.component (no typed provider found for %q)\n",
				gf.Component, gf.Type)
		}
	}
	renderModelValidate(cmd.OutOrStdout(), result, m.Order, buildDepCounts(m))
	if result.HasErrors() {
		return fmt.Errorf("model has validation errors")
	}

	if !scenariosOptedOut(m) {
		if err := checkScenariosDrift(path, reg); err != nil {
			return err
		}
	}
	if writeScenarios && !scenariosOptedOut(m) {
		if _, err := regenerateScenariosFor(cmd, m, reg, path); err != nil {
			return err
		}
	}
	return nil
}

// scenariosOptedOut returns true when the model declares
// `meta.scenarios: none` to signal "no scenarios sidecar expected"
// (typically an empty placeholder model, or one whose failure modes
// haven't been written yet).
func scenariosOptedOut(m *model.Model) bool {
	return m != nil && strings.EqualFold(m.Meta.Scenarios, "none")
}

// warnLegacyProviderRefs emits a deprecation hint for each meta.providers
// entry that still uses a bare name (pre-FQN format).
func warnLegacyProviderRefs(w io.Writer, m *model.Model) {
	for _, entry := range m.Meta.Providers {
		ref, err := model.ParseProviderRef(entry)
		if err != nil || !ref.LegacyBareName {
			continue
		}
		fmt.Fprintf(w, "⚠ model uses bare provider name %q; consider %q\n",
			ref.Name, "<namespace>/"+ref.Name+"@<version>")
	}
}

// buildDepCounts returns per-component dep counts, using -1 as the
// sentinel for "no deps but the component declares a healthy override"
// so the renderer can distinguish "leaf with explicit healthy" from
// "pure-topology leaf".
func buildDepCounts(m *model.Model) map[string]int {
	out := make(map[string]int, len(m.Components))
	for _, name := range m.Order {
		comp := m.Components[name]
		count := 0
		for _, dep := range comp.Depends {
			count += len(dep.On)
		}
		if count == 0 && len(comp.HealthyRaw) > 0 {
			out[name] = -1
		} else {
			out[name] = count
		}
	}
	return out
}

// checkScenariosDrift compares the source_hash stored in scenarios.yaml
// against the current model + types content. Returns nil when the
// sidecar is absent (nothing to check) or when hashes match; an error
// otherwise naming the file and both hashes.
func checkScenariosDrift(modelPath string, reg *providersupport.Registry) error {
	scenariosPath := scenarios.SiblingPath(modelPath)
	if _, err := os.Stat(scenariosPath); err != nil {
		return nil
	}
	_, committedHash, err := scenarios.LoadSiblingOf(modelPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", scenariosPath, err)
	}
	currentHash, err := scenarios.ComputeSourceHash(modelPath, collectTypePaths(reg))
	if err != nil {
		return err
	}
	if committedHash != currentHash {
		return fmt.Errorf("%s is stale: source_hash=%s but current content hashes to %s — run `mgtt model validate --write-scenarios` and commit", scenariosPath, committedHash, currentHash)
	}
	return nil
}

// runScenariosDriftOnly runs ONLY the scenarios.yaml drift check against
// the model at path. Skips structural / type / dep-ref validation. Used
// by `--check-scenarios` in fast CI lanes that only want to catch stale
// sidecars without paying for the full validate pass.
func runScenariosDriftOnly(cmd *cobra.Command, path string) error {
	m, err := model.Load(path)
	if err != nil {
		return err
	}
	reg, err := loadRegistryForUse()
	if err != nil {
		return err
	}

	if scenariosOptedOut(m) {
		fmt.Fprintf(cmd.OutOrStdout(), "meta.scenarios: none — drift check skipped\n")
		return nil
	}

	scenariosPath := scenarios.SiblingPath(path)
	if _, err := os.Stat(scenariosPath); err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "no scenarios.yaml at %s — drift check skipped\n", scenariosPath)
		return nil
	}
	if err := checkScenariosDrift(path, reg); err != nil {
		return err
	}
	currentHash, _ := scenarios.ComputeSourceHash(path, collectTypePaths(reg))
	fmt.Fprintf(cmd.OutOrStdout(), "scenarios.yaml up to date (source_hash=%s)\n", currentHash)
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

	reg, err := loadRegistryForUse()
	if err != nil {
		return err
	}
	entries, err := buildWorkspaceIndexEntries(cmd, reg, modelPaths)
	if err != nil {
		return err
	}
	if err := checkDuplicateModelNames(entries); err != nil {
		return err
	}
	if err := writeWorkspaceIndex(entries); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "wrote workspace index with %d model(s) to scenarios.index.yaml\n", len(entries))
	return nil
}

// buildWorkspaceIndexEntries loads each model, validates it, regenerates
// its scenarios sidecar and returns one IndexEntry per opted-in model.
func buildWorkspaceIndexEntries(cmd *cobra.Command, reg *providersupport.Registry, modelPaths []string) ([]scenarios.IndexEntry, error) {
	entries := make([]scenarios.IndexEntry, 0, len(modelPaths))
	for _, mp := range modelPaths {
		m, err := model.Load(mp)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", mp, err)
		}
		result := model.Validate(m, reg)
		if result.HasErrors() {
			return nil, fmt.Errorf("%s: model has validation errors; run `mgtt model validate %s` first", mp, mp)
		}
		if scenariosOptedOut(m) {
			continue
		}
		entry, err := regenerateScenariosFor(cmd, m, reg, mp)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func checkDuplicateModelNames(entries []scenarios.IndexEntry) error {
	seen := map[string]string{}
	for _, e := range entries {
		if prev, ok := seen[e.Name]; ok {
			return fmt.Errorf("two models with meta.name=%q: %s and %s", e.Name, prev, e.ModelPath)
		}
		seen[e.Name] = e.ModelPath
	}
	return nil
}

func writeWorkspaceIndex(entries []scenarios.IndexEntry) error {
	f, err := os.Create("scenarios.index.yaml")
	if err != nil {
		return fmt.Errorf("create scenarios.index.yaml: %w", err)
	}
	if err := scenarios.WriteIndex(f, entries); err != nil {
		_ = f.Close()
		return fmt.Errorf("write scenarios.index.yaml: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close scenarios.index.yaml: %w", err)
	}
	return nil
}

// findWorkspaceModels walks root looking for model files, skipping
// vendor/build directories. A file is a model if it is named model.yaml
// or carries the multi-file ".model.yaml" suffix (e.g. the
// system.model.yaml scaffolded by `mgtt init`).
func findWorkspaceModels(root string) ([]string, error) {
	var out []string
	err := walkModelYAMLs(root, func(path, name string) error {
		if name == "model.yaml" || strings.HasSuffix(name, ".model.yaml") {
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
	registerCommand(newModelCmd)
}

// renderModelValidate writes a human-readable validation report to w.
//
// components is the ordered list of component names to display.
// depCounts maps component name → number of direct dependencies.
func renderModelValidate(w io.Writer, result *model.ValidationResult, components []string, depCounts map[string]int) {
	compErrors, globalErrors := indexValidationErrors(result.Errors)
	maxLen := longestName(components)
	for _, name := range components {
		renderComponentValidation(w, name, compErrors[name], depCounts[name], maxLen)
	}
	for _, e := range globalErrors {
		fmt.Fprintf(w, "  %s %s\n", checkmark(false), e.Message)
	}
	renderValidationWarnings(w, result.Warnings)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s · %s · %s\n",
		pluralize(len(components), "component", "components"),
		pluralize(len(result.Errors), "error", "errors"),
		pluralize(len(result.Warnings), "warning", "warnings"),
	)
}

// indexValidationErrors splits errs into per-component and global
// buckets for the renderer.
func indexValidationErrors(errs []model.ValidationError) (perComp map[string][]model.ValidationError, global []model.ValidationError) {
	perComp = make(map[string][]model.ValidationError)
	for _, e := range errs {
		if e.Component == "" {
			global = append(global, e)
			continue
		}
		perComp[e.Component] = append(perComp[e.Component], e)
	}
	return perComp, global
}

// longestName returns the length of the longest string in names. Used
// for right-padding so columns align visually.
func longestName(names []string) int {
	maxLen := 0
	for _, n := range names {
		if len(n) > maxLen {
			maxLen = len(n)
		}
	}
	return maxLen
}

// renderComponentValidation prints one ✓ line (valid) or one ✗ line
// per error (invalid) for a component, plus a "did you mean" hint on
// typo errors.
func renderComponentValidation(w io.Writer, name string, errs []model.ValidationError, depCount, nameWidth int) {
	if len(errs) == 0 {
		fmt.Fprintf(w, "  %s %-*s  %s\n", checkmark(true), nameWidth, name, componentDesc(name, depCount))
		return
	}
	for i, e := range errs {
		firstColName := name
		if i > 0 {
			firstColName = "" // keep alignment; drop redundant name
		}
		fmt.Fprintf(w, "  %s %-*s  %s\n", checkmark(false), nameWidth, firstColName, e.Message)
		if e.Suggestion != "" {
			indent := strings.Repeat(" ", 2+1+1+nameWidth+2) // "  ✗ " + pad + "  "
			fmt.Fprintf(w, "%sdid you mean %q?\n", indent, e.Suggestion)
		}
	}
}

// renderValidationWarnings prints one "! ..." line per warning.
// Warnings are advisory and must not change exit status.
func renderValidationWarnings(w io.Writer, warnings []model.ValidationWarning) {
	for _, wn := range warnings {
		if wn.Component != "" {
			fmt.Fprintf(w, "  ! %s  %s\n", wn.Component, wn.Message)
			continue
		}
		fmt.Fprintf(w, "  ! %s\n", wn.Message)
	}
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
