package cli

import (
	"fmt"
	"io"

	"mgtt/internal/providersupport"

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
// version, and description to w.
func renderProviderLs(w io.Writer, providers []*providersupport.Provider) {
	if len(providers) == 0 {
		fmt.Fprintln(w, "  no providers installed")
		return
	}

	// Determine column widths.
	maxName := 0
	maxVersion := 0
	for _, p := range providers {
		if n := len(p.Meta.Name); n > maxName {
			maxName = n
		}
		v := "v" + p.Meta.Version
		if n := len(v); n > maxVersion {
			maxVersion = n
		}
	}

	for _, p := range providers {
		ver := "v" + p.Meta.Version
		fmt.Fprintf(w, "  %s %-*s  %-*s  %s\n",
			checkmark(true),
			maxName, p.Meta.Name,
			maxVersion, ver,
			p.Meta.Description,
		)
	}
}
