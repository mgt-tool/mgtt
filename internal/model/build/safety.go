// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package build

import (
	"errors"
	"fmt"
	"strings"
)

// ErrDeletionsRefused is returned when GateDeletions refuses a build
// that would remove components without explicit consent.
var ErrDeletionsRefused = errors.New("build refuses deletions without --allow-deletes or --tombstone")

// GateFlags captures the flags that affect deletion safety. Wired up
// to the CLI in the model-build command.
type GateFlags struct {
	AllowDeletes bool     // --allow-deletes: accept every removal
	Tombstone    []string // --tombstone=comp1,comp2: these removals are OK (still-exists-just-unseen)
}

// GateDeletions inspects the diff against the flags and returns an
// error if unauthorized removals are present. The error message names
// the offending components and suggests the two escape hatches, so
// the CLI can surface a single actionable prompt.
//
// Terraform-pattern safety: by default, writing a new model that
// removes components is refused. The committed model is the
// last-known-good reference point; removing entries from it should
// always be an intentional act signed off in a PR.
func GateDeletions(d Diff, flags GateFlags) error {
	if !d.HasDeletions() {
		return nil
	}
	if flags.AllowDeletes {
		return nil
	}
	tombstoneSet := map[string]struct{}{}
	for _, t := range flags.Tombstone {
		tombstoneSet[t] = struct{}{}
	}
	var unprotected []string
	for _, name := range d.Removed {
		if _, ok := tombstoneSet[name]; !ok {
			unprotected = append(unprotected, name)
		}
	}
	if len(unprotected) == 0 {
		return nil
	}
	msg := fmt.Sprintf("%d component(s) would be removed: %s\n"+
		"Refusing to remove components without explicit consent. Options:\n"+
		"  mgtt model build --allow-deletes\n"+
		"       # I know these are gone — accept all removals\n"+
		"  mgtt model build --tombstone=%s\n"+
		"       # keep them in the model, flag as not-currently-discovered\n"+
		"  (or investigate — a partial discovery failure looks like this)",
		len(unprotected), strings.Join(unprotected, ", "), strings.Join(unprotected, ","))
	return fmt.Errorf("%w: %s", ErrDeletionsRefused, msg)
}
