// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/mgt-tool/mgtt/internal/providersupport"

	"github.com/spf13/cobra"
)

func newProviderInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <name> [type]",
		Short: "Inspect a provider or a specific type within it",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			typeName := ""
			if len(args) == 2 {
				typeName = args[1]
			}
			p, err := providersupport.LoadForUse(name)
			if err != nil {
				return fmt.Errorf("provider %q: %w", name, err)
			}
			renderProviderInspect(cmd.OutOrStdout(), p, typeName)
			return nil
		},
	}
}

// renderProviderInspect writes provider details to w. When typeName is empty,
// an overview is shown (name, version, description, list of types). When
// typeName is non-empty, detailed type information is shown.
func renderProviderInspect(w io.Writer, p *providersupport.Provider, typeName string) {
	if typeName == "" {
		renderProviderOverview(w, p)
		return
	}
	t, ok := p.Types[typeName]
	if !ok {
		fmt.Fprintf(w, "  type %q not found in provider %q\n", typeName, p.Meta.Name)
		return
	}
	renderTypeDetail(w, p, t)
}

// renderProviderOverview renders the provider summary view.
func renderProviderOverview(w io.Writer, p *providersupport.Provider) {
	renderOverviewHeader(w, p)
	renderMapSection(w, "needs", p.Runtime.Needs, formatNameConstraint)
	renderMapSection(w, "backends", p.Runtime.Backends, formatNameConstraint)
	renderListSection(w, "install methods", installMethodLines(p))
	if p.Runtime.NetworkMode != "" && p.Runtime.NetworkMode != "bridge" {
		fmt.Fprintf(w, "  network_mode: %s\n", p.Runtime.NetworkMode)
	}
	renderMapSection(w, "requires", p.Meta.Requires, formatKV)
	fmt.Fprintln(w)
	renderTypesList(w, p.Types)
}

// renderOverviewHeader prints name / version / description / tags /
// posture (+ writes-note when writes are declared).
func renderOverviewHeader(w io.Writer, p *providersupport.Provider) {
	fmt.Fprintf(w, "  provider:    %s\n", p.Meta.Name)
	fmt.Fprintf(w, "  version:     v%s\n", p.Meta.Version)
	fmt.Fprintf(w, "  description: %s\n", p.Meta.Description)
	if len(p.Meta.Tags) > 0 {
		fmt.Fprintf(w, "  tags:        %s\n", strings.Join(p.Meta.Tags, ", "))
	}
	posture := "read-only"
	if !p.ReadOnly {
		posture = "writes"
	}
	fmt.Fprintf(w, "  posture:     %s\n", posture)
	if !p.ReadOnly && strings.TrimSpace(p.WritesNote) != "" {
		fmt.Fprintf(w, "  writes-note: %s\n", firstLine(p.WritesNote))
	}
}

// renderMapSection renders a named sub-block ("needs:", "requires:"…)
// with one sorted "key value" line per entry, using fmtLine to build
// the display string. No-op for empty maps.
func renderMapSection(w io.Writer, label string, m map[string]string, fmtLine func(name, val string) string) {
	if len(m) == 0 {
		return
	}
	fmt.Fprintf(w, "  %s:\n", label)
	for _, name := range slices.Sorted(maps.Keys(m)) {
		fmt.Fprintf(w, "      %s\n", fmtLine(name, m[name]))
	}
}

// renderListSection prints a named sub-block with one line per entry;
// no-op for empty lists.
func renderListSection(w io.Writer, label string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(w, "  %s:\n", label)
	for _, line := range lines {
		fmt.Fprintf(w, "      %s\n", line)
	}
}

// renderTypesList prints the "types (N):" block: one line per type in
// sorted-name order with a short description.
func renderTypesList(w io.Writer, types map[string]*providersupport.Type) {
	names := make([]string, 0, len(types))
	for k := range types {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Fprintf(w, "  types (%d):\n", len(names))
	for _, name := range names {
		desc := types[name].Description
		if desc == "" {
			desc = "-"
		}
		fmt.Fprintf(w, "    %-20s  %s\n", name, desc)
	}
}

// formatKV renders a "name: value" pair — used for the requires block.
func formatKV(name, val string) string { return name + ": " + val }

// renderTypeDetail renders the detailed view for a single type.
func renderTypeDetail(w io.Writer, p *providersupport.Provider, t *providersupport.Type) {
	renderTypeHeader(w, p, t)
	renderFactsSection(w, t.Facts)
	renderHealthySection(w, t.HealthyRaw)
	renderStatesSection(w, t)
	renderFailureModesSection(w, t.FailureModes)
}

func renderTypeHeader(w io.Writer, p *providersupport.Provider, t *providersupport.Type) {
	fmt.Fprintf(w, "  provider:  %s\n", p.Meta.Name)
	fmt.Fprintf(w, "  type:      %s\n", t.Name)
	if t.Description != "" {
		fmt.Fprintf(w, "  desc:      %s\n", t.Description)
	}
	fmt.Fprintln(w)
}

func renderFactsSection(w io.Writer, factsByName map[string]*providersupport.FactSpec) {
	if len(factsByName) == 0 {
		return
	}
	names := make([]string, 0, len(factsByName))
	for k := range factsByName {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Fprintln(w, "  facts:")
	for _, name := range names {
		fs := factsByName[name]
		ttl := fs.TTL.String()
		if fs.TTL == 0 {
			ttl = "~"
		}
		cost := fs.Probe.Cost
		if cost == "" {
			cost = "~"
		}
		fmt.Fprintf(w, "    %-20s  type: %-14s  ttl: %-6s  cost: %s\n",
			name, fs.TypeName, ttl, cost)
	}
	fmt.Fprintln(w)
}

func renderHealthySection(w io.Writer, healthy []string) {
	if len(healthy) == 0 {
		return
	}
	fmt.Fprintln(w, "  healthy:")
	for _, cond := range healthy {
		fmt.Fprintf(w, "    - %s\n", cond)
	}
	fmt.Fprintln(w)
}

func renderStatesSection(w io.Writer, t *providersupport.Type) {
	if len(t.States) == 0 {
		return
	}
	fmt.Fprintln(w, "  states:")
	for _, s := range t.States {
		marker := " "
		if s.Name == t.DefaultActiveState {
			marker = "*"
		}
		desc := s.Description
		if desc == "" {
			desc = "-"
		}
		fmt.Fprintf(w, "   %s %-20s  when: %-40s  desc: %s\n",
			marker, s.Name, s.WhenRaw, desc)
	}
	fmt.Fprintln(w)
}

func renderFailureModesSection(w io.Writer, fm map[string][]string) {
	if len(fm) == 0 {
		return
	}
	states := make([]string, 0, len(fm))
	for k := range fm {
		states = append(states, k)
	}
	sort.Strings(states)
	fmt.Fprintln(w, "  failure_modes:")
	for _, state := range states {
		fmt.Fprintf(w, "    %-20s  can_cause: %s\n", state, strings.Join(fm[state], ", "))
	}
}

// firstLine returns the first non-blank line of s, trimmed. Used so a
// multi-line writes_note renders as one line in the summary view.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// formatNameConstraint joins a capability/backend name with its optional
// version constraint: "aws >=2.13" or just "kubectl" when the author
// left the constraint empty (list-shorthand form).
func formatNameConstraint(name, constraint string) string {
	if constraint == "" {
		return name
	}
	return name + " " + constraint
}

// installMethodLines returns a short description line per declared
// install method. Empty when neither is set (the parser rejects that
// case, so callers should see at least one line in practice).
func installMethodLines(p *providersupport.Provider) []string {
	var out []string
	if s := p.Install.Source; s != nil {
		out = append(out, fmt.Sprintf("source  (build: %s; clean: %s)", s.Build, s.Clean))
	}
	if img := p.Install.Image; img != nil {
		repo := img.Repository
		if repo == "" {
			repo = "(resolved from registry)"
		}
		out = append(out, fmt.Sprintf("image   (%s)", repo))
	}
	return out
}
