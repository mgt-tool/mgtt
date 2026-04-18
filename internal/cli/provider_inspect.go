package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mgt-tool/mgtt/internal/providersupport"

	"github.com/spf13/cobra"
)

var providerInspectCmd = &cobra.Command{
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

func init() {
	providerCmd.AddCommand(providerInspectCmd)
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
	if len(p.Runtime.Needs) > 0 {
		fmt.Fprintf(w, "  needs:       %s\n", strings.Join(sortedNeeds(p.Runtime.Needs), ", "))
	}
	if p.Runtime.NetworkMode != "" && p.Runtime.NetworkMode != "bridge" {
		fmt.Fprintf(w, "  network:     %s\n", p.Runtime.NetworkMode)
	}
	fmt.Fprintln(w)

	// Sorted type names.
	typeNames := make([]string, 0, len(p.Types))
	for k := range p.Types {
		typeNames = append(typeNames, k)
	}
	sort.Strings(typeNames)

	fmt.Fprintf(w, "  types (%d):\n", len(typeNames))
	for _, name := range typeNames {
		t := p.Types[name]
		desc := t.Description
		if desc == "" {
			desc = "-"
		}
		fmt.Fprintf(w, "    %-20s  %s\n", name, desc)
	}
}

// renderTypeDetail renders the detailed view for a single type.
func renderTypeDetail(w io.Writer, p *providersupport.Provider, t *providersupport.Type) {
	fmt.Fprintf(w, "  provider:  %s\n", p.Meta.Name)
	fmt.Fprintf(w, "  type:      %s\n", t.Name)
	if t.Description != "" {
		fmt.Fprintf(w, "  desc:      %s\n", t.Description)
	}
	fmt.Fprintln(w)

	// Facts — sorted by name.
	if len(t.Facts) > 0 {
		factNames := make([]string, 0, len(t.Facts))
		for k := range t.Facts {
			factNames = append(factNames, k)
		}
		sort.Strings(factNames)

		fmt.Fprintln(w, "  facts:")
		for _, name := range factNames {
			fs := t.Facts[name]
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

	// Healthy conditions.
	if len(t.HealthyRaw) > 0 {
		fmt.Fprintln(w, "  healthy:")
		for _, cond := range t.HealthyRaw {
			fmt.Fprintf(w, "    - %s\n", cond)
		}
		fmt.Fprintln(w)
	}

	// States (in declaration order).
	if len(t.States) > 0 {
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

	// Failure modes — sorted by state name.
	if len(t.FailureModes) > 0 {
		fmStates := make([]string, 0, len(t.FailureModes))
		for k := range t.FailureModes {
			fmStates = append(fmStates, k)
		}
		sort.Strings(fmStates)

		fmt.Fprintln(w, "  failure_modes:")
		for _, state := range fmStates {
			causes := t.FailureModes[state]
			fmt.Fprintf(w, "    %-20s  can_cause: %s\n", state, strings.Join(causes, ", "))
		}
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
