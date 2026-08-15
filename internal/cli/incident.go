// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mgt-tool/mgtt/internal/engine/strategy"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"

	"github.com/spf13/cobra"
)

func newIncidentStartCmd() *cobra.Command {
	var id, modelPath string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new incident",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := model.Load(modelPath)
			if err != nil {
				return fmt.Errorf("load model: %w", err)
			}
			inc, err := incident.Start(m.Meta.Name, m.Meta.Version, id)
			if err != nil {
				return err
			}
			renderIncidentStart(cmd.OutOrStdout(), inc)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "incident ID (auto-generated if empty)")
	cmd.Flags().StringVar(&modelPath, "model", "system.model.yaml", "path to system.model.yaml")
	return cmd
}

func newIncidentEndCmd() *cobra.Command {
	var suggestScenarios bool
	cmd := &cobra.Command{
		Use:   "end",
		Short: "End the current incident",
		RunE: func(cmd *cobra.Command, args []string) error {
			inc, err := incident.End()
			if err != nil {
				return err
			}
			renderIncidentEnd(cmd.OutOrStdout(), inc, inc.Store)
			if suggestScenarios {
				if err := emitScenarioSuggestions(cmd.OutOrStdout(), inc); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: --suggest-scenarios: %v\n", err)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&suggestScenarios, "suggest-scenarios", false, "emit a scenarios patch file proposing new chains based on this incident")
	return cmd
}

func init() {
	registerCommand(func() *cobra.Command {
		incidentCmd := &cobra.Command{
			Use:   "incident",
			Short: "Manage troubleshooting incidents",
		}
		incidentCmd.AddCommand(newIncidentStartCmd())
		incidentCmd.AddCommand(newIncidentEndCmd())
		return incidentCmd
	})
}

// renderIncidentStart renders a confirmation that an incident has been started.
func renderIncidentStart(w io.Writer, inc *incident.Incident) {
	fmt.Fprintf(w, "  %s %s started\n", checkmark(true), inc.ID)
	fmt.Fprintf(w, "    model: %s v%s\n", inc.Model, inc.Version)
	fmt.Fprintf(w, "    state: %s\n", inc.StateFile)
}

// renderIncidentEnd renders an incident closure summary.
func renderIncidentEnd(w io.Writer, inc *incident.Incident, store *facts.Store) {
	duration := inc.Ended.Sub(inc.Started)
	fmt.Fprintf(w, "  %s %s ended\n", checkmark(true), inc.ID)
	fmt.Fprintf(w, "    duration: %s\n", duration.Round(time.Second))

	// Count facts.
	components := store.AllComponents()
	sort.Strings(components)
	total := 0
	for _, c := range components {
		total += len(store.FactsFor(c))
	}
	fmt.Fprintf(w, "    facts:    %s across %s\n",
		pluralize(total, "fact", "facts"),
		pluralize(len(components), "component", "components"),
	)
}

// suggestionLoader is the hook the test suite overrides to inject a
// synthetic (model, registry, modelPath) tuple without reading YAML off
// disk. Returning a synthetic modelPath lets tests still anchor the
// sibling scenarios.yaml lookup at a real directory.
type suggestionLoader func(modelName string) (*model.Model, *providersupport.Registry, string, error)

// defaultSuggestionLoader is the production implementation: walk cwd for
// a model.yaml whose meta.name matches, then load it with the active
// registry. Tests replace this via withSuggestionLoader.
func defaultSuggestionLoader(modelName string) (*model.Model, *providersupport.Registry, string, error) {
	modelPath, err := findModelByName(modelName)
	if err != nil {
		return nil, nil, "", fmt.Errorf("locate model: %w", err)
	}
	m, reg, err := loadModelAndRegistry(modelPath)
	if err != nil {
		return nil, nil, "", err
	}
	return m, reg, modelPath, nil
}

// suggestionLoaderHook is the active loader. Production code leaves it
// pointing at defaultSuggestionLoader; tests overwrite it via
// withSuggestionLoader to inject fixtures.
var suggestionLoaderHook suggestionLoader = defaultSuggestionLoader

// emitScenarioSuggestions is the body of `mgtt incident end --suggest-scenarios`.
// It locates the model file referenced by the incident, replays the fact store
// through each type's non-default state predicates, and writes a review-patch
// file describing the observed chain when it's not already represented in
// scenarios.yaml.
func emitScenarioSuggestions(out io.Writer, inc *incident.Incident) error {
	m, reg, modelPath, err := suggestionLoaderHook(inc.Model)
	if err != nil {
		return err
	}

	// Load committed scenarios.yaml if present; otherwise treat as empty.
	existing, _, _ := scenarios.LoadSiblingOf(modelPath)

	observed, err := synthesizeObservedChain(inc.Store, m, reg)
	if err != nil {
		return fmt.Errorf("synthesize chain: %w", err)
	}
	if len(observed) < 2 {
		fmt.Fprintf(out, "incident has no non-trivial chain to suggest (only %d step(s) observed).\n", len(observed))
		return nil
	}

	if matches := findMatching(existing, observed); matches != "" {
		fmt.Fprintf(out, "observed chain matches existing scenario %s — no suggestion needed.\n", matches)
		return nil
	}

	patchDir := ".mgtt/pending-scenarios"
	if err := os.MkdirAll(patchDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", patchDir, err)
	}
	patchPath := filepath.Join(patchDir, inc.ID+".patch")
	f, err := os.Create(patchPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", patchPath, err)
	}
	if err := writeSuggestionPatch(f, observed, inc.ID); err != nil {
		_ = f.Close()
		return fmt.Errorf("write patch: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", patchPath, err)
	}
	fmt.Fprintf(out, "wrote %s — review, merge into %s, and run `mgtt model validate --write-scenarios` to regenerate.\n", patchPath, scenarios.SiblingPath(modelPath))
	return nil
}

// loadModelAndRegistry loads the model at path and builds a provider
// registry including the generic fallback. It does not resolve provider
// refs — suggestion emission only needs type resolution against
// already-installed providers.
func loadModelAndRegistry(path string) (*model.Model, *providersupport.Registry, error) {
	m, err := model.Load(path)
	if err != nil {
		return nil, nil, fmt.Errorf("load model: %w", err)
	}
	reg, err := loadRegistryForUse()
	if err != nil {
		return nil, nil, err
	}
	return m, reg, nil
}

// findModelByName walks cwd for any file literally named model.yaml or
// system.model.yaml and returns the first one whose meta.name matches.
// Returns an error if none match.
func findModelByName(name string) (string, error) {
	var found string
	err := walkModelYAMLs(".", func(path, base string) error {
		if base != "model.yaml" && base != "system.model.yaml" {
			return nil
		}
		m, loadErr := model.Load(path)
		if loadErr != nil {
			return nil // keep walking; another candidate may still match
		}
		if m.Meta.Name == name && found == "" {
			found = path
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("no model.yaml with meta.name=%q found under cwd", name)
	}
	return found, nil
}

// synthesizeObservedChain evaluates every component's non-default state
// predicates against the fact store and returns an ordered chain of
// steps — implicated root upstream first, symptom downstream last.
//
// A component is implicated only if the store has facts for it AND one
// non-default state's when-predicate evaluates cleanly to true. Steps are
// ordered by dependency depth from the entry point; for a failure that
// propagates root → symptom, deepest (furthest from entry) is the root
// and depth-0 is the symptom.
func synthesizeObservedChain(store *facts.Store, m *model.Model, reg *providersupport.Registry) ([]scenarios.Step, error) {
	implicated := implicatedComponents(store, m, reg)
	order := orderByDependencyDepth(m, implicated)
	steps := make([]scenarios.Step, 0, len(order))
	for i, compName := range order {
		step := buildObservedStep(m, reg, compName, implicated[compName], i == len(order)-1)
		steps = append(steps, step)
	}
	return steps, nil
}

// implicatedComponents scans every component with recorded facts and
// picks the first non-default state whose predicate evaluates true.
// Returns component → state name for those with a matching state.
func implicatedComponents(store *facts.Store, m *model.Model, reg *providersupport.Registry) map[string]string {
	implicated := map[string]string{}
	for compName, comp := range m.Components {
		if store == nil || store.FactsFor(compName) == nil {
			continue
		}
		t, _, err := comp.ResolveType(m, reg)
		if err != nil || t == nil {
			continue
		}
		for _, st := range t.States {
			if st.Name == t.DefaultActiveState || st.When == nil {
				continue
			}
			ok, err := strategy.EvalStatePredicate(st.When, store, compName)
			if err == nil && ok {
				implicated[compName] = st.Name
				break
			}
		}
	}
	return implicated
}

// buildObservedStep assembles one scenario step. The terminal step
// records every declared fact name in Observes; non-terminal steps
// record the first FailureModes emit for the edge to the next step.
func buildObservedStep(m *model.Model, reg *providersupport.Registry, compName, state string, terminal bool) scenarios.Step {
	step := scenarios.Step{Component: compName, State: state}
	comp := m.Components[compName]
	t, _, _ := comp.ResolveType(m, reg)
	if t == nil {
		return step
	}
	if terminal {
		if len(t.Facts) > 0 {
			names := make([]string, 0, len(t.Facts))
			for fn := range t.Facts {
				names = append(names, fn)
			}
			sort.Strings(names)
			step.Observes = names
		}
		return step
	}
	if emits := t.FailureModes[state]; len(emits) > 0 {
		step.EmitsOnEdge = emits[0]
	}
	return step
}

// orderByDependencyDepth orders the implicated components by BFS depth
// from the model's entry point, deepest first. In mgtt an edge
// "A depends on B" means A's health depends on B's — so B (upstream
// cause) sits at greater BFS depth than A (downstream symptom). Emitting
// deepest-first yields the conventional root → symptom chain.
func orderByDependencyDepth(m *model.Model, implicated map[string]string) []string {
	depth := bfsDepthsFromEntry(m)
	return sortImplicatedByDepth(implicated, depth)
}

func bfsDepthsFromEntry(m *model.Model) map[string]int {
	entry := m.EntryPoint()
	depth := map[string]int{}
	if entry == "" {
		return depth
	}
	depth[entry] = 0
	queue := []string{entry}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		comp := m.Components[c]
		if comp == nil {
			continue
		}
		for _, dep := range comp.Depends {
			for _, target := range dep.On {
				if _, seen := depth[target]; seen {
					continue
				}
				depth[target] = depth[c] + 1
				queue = append(queue, target)
			}
		}
	}
	return depth
}

func sortImplicatedByDepth(implicated map[string]string, depth map[string]int) []string {
	type dc struct {
		name  string
		depth int
	}
	ordered := make([]dc, 0, len(implicated))
	for name := range implicated {
		d, ok := depth[name]
		if !ok {
			// Unreachable from entry — treat as least-significant so it
			// still appears but doesn't disturb the main chain.
			d = -1
		}
		ordered = append(ordered, dc{name, d})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].depth != ordered[j].depth {
			return ordered[i].depth > ordered[j].depth // deepest first = upstream
		}
		return ordered[i].name < ordered[j].name
	})
	out := make([]string, len(ordered))
	for i, e := range ordered {
		out[i] = e.name
	}
	return out
}

// findMatching returns the ID of the first existing scenario whose chain
// has the same (component, state) sequence as observed, or "" if none.
// Step metadata (emits_on_edge, observes) is ignored — a match on the
// component/state tuple is sufficient because those fields are derived.
func findMatching(existing []scenarios.Scenario, observed []scenarios.Step) string {
	for _, s := range existing {
		if len(s.Chain) != len(observed) {
			continue
		}
		match := true
		for i, step := range s.Chain {
			if step.Component != observed[i].Component || step.State != observed[i].State {
				match = false
				break
			}
		}
		if match {
			return s.ID
		}
	}
	return ""
}

// writeSuggestionPatch emits a human-reviewable patch describing the
// observed chain. Deliberately plain-text (not structural YAML) — it is
// a diff the operator merges by hand, not an artifact mgtt reads back.
func writeSuggestionPatch(w io.Writer, observed []scenarios.Step, incidentID string) error {
	if len(observed) == 0 {
		return nil
	}
	root := observed[0]
	fmt.Fprintf(w, "# .mgtt/pending-scenarios/%s.patch\n", incidentID)
	fmt.Fprintln(w, "# Proposed addition to scenarios.yaml — REVIEW before merging.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "+ - id: <new>       # mgtt model validate --write-scenarios will assign the real ID")
	fmt.Fprintf(w, "+   root: { component: %s, state: %s }\n", root.Component, root.State)
	fmt.Fprintln(w, "+   chain:")
	for _, step := range observed {
		switch {
		case len(step.Observes) > 0:
			fmt.Fprintf(w, "+     - { component: %s, state: %s, observes: [%s] }\n",
				step.Component, step.State, strings.Join(step.Observes, ", "))
		case step.EmitsOnEdge != "":
			fmt.Fprintf(w, "+     - { component: %s, state: %s, emits_on_edge: %s }\n",
				step.Component, step.State, step.EmitsOnEdge)
		default:
			fmt.Fprintf(w, "+     - { component: %s, state: %s }\n", step.Component, step.State)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "# Observed during: %s\n", incidentID)
	fmt.Fprintln(w, "# To accept: add triggered_by / can_cause labels to your type YAMLs as needed,")
	fmt.Fprintln(w, "# then run `mgtt model validate --write-scenarios` to regenerate scenarios.yaml.")
	fmt.Fprintln(w, "# Delete this file once merged.")
	return nil
}
