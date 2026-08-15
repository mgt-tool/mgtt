// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/mgt-tool/mgtt/internal/providersupport"

	"github.com/spf13/cobra"
)

func newProviderLsCmd() *cobra.Command {
	return &cobra.Command{
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
}

// providerRow holds the pre-rendered strings for one line of the
// provider-ls table. Computed once so the table's column widths can
// be derived in a single pass.
type providerRow struct {
	displayName string
	ver         string
	method      string
	caps        string
	description string
}

// renderProviderLs writes one line per provider with a checkmark, name,
// version, install method, image capabilities, and description to w.
func renderProviderLs(w io.Writer, providers []*providersupport.Provider) {
	if len(providers) == 0 {
		fmt.Fprintln(w, "  no providers installed")
		return
	}
	rows := make([]providerRow, 0, len(providers))
	for _, p := range providers {
		rows = append(rows, buildProviderRow(p))
	}
	widths := providerLsColumnWidths(rows)
	for _, r := range rows {
		fmt.Fprintf(w, "  %s %-*s  %-*s  %-*s  %-*s  %s\n",
			checkmark(true),
			widths.name, r.displayName,
			widths.ver, r.ver,
			widths.method, r.method,
			widths.caps, r.caps,
			r.description,
		)
	}
}

// buildProviderRow assembles the display row for one provider: install
// metadata + capability list + namespace-qualified display name.
func buildProviderRow(p *providersupport.Provider) providerRow {
	return providerRow{
		displayName: providerDisplayName(p),
		ver:         "v" + p.Meta.Version,
		method:      providerInstallMethod(p),
		caps:        providerCapsLabel(p),
		description: p.Meta.Description,
	}
}

// providerDisplayName returns the namespace-qualified install name
// (e.g. "mgt-tool/kubernetes") when install metadata is available,
// else the bare meta.name.
func providerDisplayName(p *providersupport.Provider) string {
	dir := providersupport.ProviderDir(p.Meta.Name)
	if dir == "" {
		return p.Meta.Name
	}
	meta, err := providersupport.ReadInstallMeta(dir)
	if err != nil || meta.Namespace == "" {
		return p.Meta.Name
	}
	return meta.Namespace + "/" + p.Meta.Name
}

// providerInstallMethod returns "git" / "image" / "?" depending on the
// install-meta file's Method field. "?" covers the legacy
// pre-metadata case and any read error.
func providerInstallMethod(p *providersupport.Provider) string {
	dir := providersupport.ProviderDir(p.Meta.Name)
	if dir == "" {
		return "?"
	}
	meta, err := providersupport.ReadInstallMeta(dir)
	if err != nil {
		return "?"
	}
	return string(meta.Method)
}

// providerCapsLabel renders the image-caps vocabulary as "[a, b, c]".
// Always-rendered for parity between git and image installs — the
// contract is the same in either case.
func providerCapsLabel(p *providersupport.Provider) string {
	if len(p.Runtime.Needs) == 0 {
		return "-"
	}
	return "[" + strings.Join(slices.Sorted(maps.Keys(p.Runtime.Needs)), ", ") + "]"
}

type providerLsWidths struct{ name, ver, method, caps int }

// providerLsColumnWidths picks the narrowest column widths that still
// align every row.
func providerLsColumnWidths(rows []providerRow) providerLsWidths {
	w := providerLsWidths{method: len("image")} // "git" (3) or "image" (5)
	for _, r := range rows {
		if n := len(r.displayName); n > w.name {
			w.name = n
		}
		if n := len(r.ver); n > w.ver {
			w.ver = n
		}
		if n := len(r.caps); n > w.caps {
			w.caps = n
		}
	}
	return w
}
