// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"os"
	"path/filepath"
)

// walkSkipDirs are directories that can never contain source models —
// skipping them is both a correctness hint and a perf cliff avoidance.
var walkSkipDirs = map[string]bool{
	".git":         true,
	".mgtt":        true,
	"node_modules": true,
	"site":         true,
}

// walkModelYAMLs walks root and invokes visit(path, basename) for every
// regular file, skipping vendor/build dirs. The visitor picks which
// filenames it cares about (model.yaml, system.model.yaml, ...).
func walkModelYAMLs(root string, visit func(path, base string) error) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if walkSkipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		return visit(path, info.Name())
	})
}
