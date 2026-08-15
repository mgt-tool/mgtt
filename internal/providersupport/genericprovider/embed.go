// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package genericprovider ships an embedded built-in provider that serves
// as the fallback for components whose declared type isn't found in any
// installed provider. Callers register it into a providersupport.Registry
// (typically the CLI's model-load path) to enable the fallback behaviour.
package genericprovider

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/mgt-tool/mgtt/internal/providersupport"
)

//go:embed manifest.yaml types/*.yaml
var files embed.FS

// Name is the meta.name of the embedded generic provider. Exported so
// callers (e.g. Registry.ResolveType fallback) can reference it without
// hardcoding the string.
const Name = "generic"

// Register loads the embedded generic provider into reg. Registration is
// idempotent — re-registration overwrites any prior entry, matching
// Registry.Register's own semantics.
func Register(reg *providersupport.Registry) error {
	return loadFromFS(reg, files)
}

func loadFromFS(reg *providersupport.Registry, fsys embed.FS) error {
	manifestBytes, err := fsys.ReadFile("manifest.yaml")
	if err != nil {
		return fmt.Errorf("generic provider: read manifest.yaml: %w", err)
	}
	p, err := providersupport.LoadFromBytes(manifestBytes)
	if err != nil {
		return fmt.Errorf("generic provider: parse manifest: %w", err)
	}

	entries, err := fs.ReadDir(fsys, "types")
	if err != nil {
		return fmt.Errorf("generic provider: read types/: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := fsys.ReadFile("types/" + e.Name())
		if err != nil {
			return fmt.Errorf("generic provider: read types/%s: %w", e.Name(), err)
		}
		typeName := strings.TrimSuffix(e.Name(), ".yaml")
		t, err := providersupport.LoadTypeFromBytes(typeName, data)
		if err != nil {
			return fmt.Errorf("generic provider: parse type %s: %w", typeName, err)
		}
		// Embedded types have no on-disk source; SourcePath stays empty so
		// consumers that scan the filesystem for types skip them (this is
		// the correct behaviour — the types are built into the binary).
		t.SourcePath = ""
		if p.Types == nil {
			p.Types = make(map[string]*providersupport.Type)
		}
		p.Types[typeName] = t
	}
	reg.Register(p)
	return nil
}
