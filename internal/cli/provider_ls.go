package cli

import (
	"fmt"
	"io"

	"github.com/mgt-tool/mgtt/internal/providersupport"

	"github.com/spf13/cobra"
)

var providerLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List available providers",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		names := providersupport.ListEmbedded()
		var providers []*providersupport.Provider
		for _, name := range names {
			p, err := providersupport.LoadEmbedded(name)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not load provider %q: %v\n", name, err)
				continue
			}
			providers = append(providers, p)
		}
		renderProviderLs(cmd.OutOrStdout(), providers)
		return nil
	},
}

func init() {
	providerCmd.AddCommand(providerLsCmd)
}

// renderProviderLs writes one line per provider with a checkmark, name,
// version, install method, image capabilities, and description to w.
func renderProviderLs(w io.Writer, providers []*providersupport.Provider) {
	if len(providers) == 0 {
		fmt.Fprintln(w, "  no providers installed")
		return
	}

	// Load install metadata for all providers first so we can compute column widths.
	type providerRow struct {
		displayName string
		ver         string
		method      string
		caps        string
		description string
	}
	rows := make([]providerRow, 0, len(providers))
	for _, p := range providers {
		displayName := p.Meta.Name
		providerDir := providersupport.ProviderDir(p.Meta.Name)
		method := "?"
		if providerDir != "" {
			meta, err := providersupport.ReadInstallMeta(providerDir)
			if err == nil {
				method = string(meta.Method)
				if meta.Namespace != "" {
					displayName = meta.Namespace + "/" + p.Meta.Name
				}
			}
			// On error: show "?" and continue (don't abort the listing)
		}
		// Capabilities come from manifest.yaml's image.needs. They're
		// most meaningful for image-installed providers (they drive the
		// docker-run flags), but rendering them for git installs too is
		// fine: it's still a declared part of the provider contract.
		caps := "-"
		if len(p.Needs) > 0 {
			caps = "[" + joinNeeds(p.Needs) + "]"
		}
		rows = append(rows, providerRow{
			displayName: displayName,
			ver:         "v" + p.Meta.Version,
			method:      method,
			caps:        caps,
			description: p.Meta.Description,
		})
	}

	// Determine column widths.
	maxName := 0
	maxVersion := 0
	maxMethod := len("image") // "git" (3) or "image" (5); "image" is wider
	maxCaps := 0
	for _, r := range rows {
		if n := len(r.displayName); n > maxName {
			maxName = n
		}
		if n := len(r.ver); n > maxVersion {
			maxVersion = n
		}
		if n := len(r.caps); n > maxCaps {
			maxCaps = n
		}
	}

	for _, r := range rows {
		fmt.Fprintf(w, "  %s %-*s  %-*s  %-*s  %-*s  %s\n",
			checkmark(true),
			maxName, r.displayName,
			maxVersion, r.ver,
			maxMethod, r.method,
			maxCaps, r.caps,
			r.description,
		)
	}
}

// joinNeeds renders a cap list as "a, b, c". Dedicated helper to keep
// the render loop terse and to avoid importing strings just for Join.
func joinNeeds(needs []string) string {
	out := ""
	for i, n := range needs {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
