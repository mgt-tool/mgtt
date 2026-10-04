// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/modeldiff"

	"github.com/spf13/cobra"
)

func newModelDiffCmd() *cobra.Command {
	var base string
	cmd := &cobra.Command{
		Use:   "diff [old] [new]",
		Short: "Compare two revisions of a model by what they mean",
		Long: "Compare two revisions of a model: components added and removed,\n" +
			"dependencies, effective health rules (type defaults and overrides\n" +
			"together), vars -- and which user-facing symptoms each component's\n" +
			"failure reaches, which a text diff cannot show.\n\n" +
			"  mgtt model diff old.yaml new.yaml\n" +
			"  mgtt model diff --base main            # this model against main\n" +
			"  mgtt model diff --base HEAD~1 path.yaml",
		Args:         cobra.MaximumNArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var oldM, newM *model.Model
			var oldName, newName string
			var err error
			switch {
			case base != "":
				if len(args) > 1 {
					return fmt.Errorf("with --base, give at most the model path")
				}
				path := ""
				if len(args) == 1 {
					path = args[0]
				}
				if path, err = resolveModelPath(path); err != nil {
					return err
				}
				if oldM, err = modelAtRevision(base, path); err != nil {
					return err
				}
				if newM, err = model.Load(path); err != nil {
					return err
				}
				oldName, newName = base+":"+path, path
			case len(args) == 2:
				if oldM, err = model.Load(args[0]); err != nil {
					return err
				}
				if newM, err = model.Load(args[1]); err != nil {
					return err
				}
				oldName, newName = args[0], args[1]
			default:
				return fmt.Errorf("give two model files, or --base <git revision>")
			}
			reg, err := loadRegistryForUse()
			if err != nil {
				return err
			}
			renderModelDiff(cmd.OutOrStdout(), oldName, newName, modeldiff.Compare(oldM, newM, reg))
			return nil
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "git revision to compare the model against (e.g. main, HEAD~1)")
	return cmd
}

// modelAtRevision loads path as it was at a git revision.
func modelAtRevision(rev, path string) (*model.Model, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	out, err := exec.Command("git", "-C", filepath.Dir(abs), "show", rev+":./"+filepath.Base(abs)).Output()
	if err != nil {
		msg := err.Error()
		if ee, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("read %s at %s: %s", path, rev, msg)
	}
	return model.LoadBytes(out, "")
}

func renderModelDiff(w io.Writer, oldName, newName string, d *modeldiff.Diff) {
	fmt.Fprintf(w, "%s → %s\n\n", oldName, newName)
	if d.Empty() {
		fmt.Fprintln(w, "  no difference in meaning.")
		return
	}
	for _, c := range d.Added {
		fmt.Fprintf(w, "  + %s\n", c)
	}
	for _, c := range d.Removed {
		fmt.Fprintf(w, "  - %s\n", c)
	}
	for _, c := range d.Changed {
		fmt.Fprintf(w, "  ~ %s\n", c.Name)
		for _, line := range c.Changes {
			fmt.Fprintf(w, "      %s\n", line)
		}
	}
	if len(d.Reach) > 0 {
		fmt.Fprintln(w, "\n  What a failure reaches, for users:")
		for _, r := range d.Reach {
			var parts []string
			if len(r.Gained) > 0 {
				parts = append(parts, "now reaches "+strings.Join(r.Gained, ", "))
			}
			if len(r.Lost) > 0 {
				parts = append(parts, "no longer reaches "+strings.Join(r.Lost, ", "))
			}
			fmt.Fprintf(w, "    %s fails: %s\n", r.Component, strings.Join(parts, "; "))
		}
	}
	if d.OldScenarios != d.NewScenarios {
		fmt.Fprintf(w, "\n  scenarios: %d → %d\n", d.OldScenarios, d.NewScenarios)
	}
}
