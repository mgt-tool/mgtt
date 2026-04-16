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
// version, install method, and description to w.
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
		rows = append(rows, providerRow{
			displayName: displayName,
			ver:         "v" + p.Meta.Version,
			method:      method,
			description: p.Meta.Description,
		})
	}

	// Determine column widths.
	maxName := 0
	maxVersion := 0
	maxMethod := len("image") // "git" (3) or "image" (5); "image" is wider
	for _, r := range rows {
		if n := len(r.displayName); n > maxName {
			maxName = n
		}
		if n := len(r.ver); n > maxVersion {
			maxVersion = n
		}
	}

	for _, r := range rows {
		fmt.Fprintf(w, "  %s %-*s  %-*s  %-*s  %s\n",
			checkmark(true),
			maxName, r.displayName,
			maxVersion, r.ver,
			maxMethod, r.method,
			r.description,
		)
	}
}
