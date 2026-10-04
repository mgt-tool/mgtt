// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package dispatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
)

func TestRecord(t *testing.T) {
	cases := []struct {
		name       string
		res        probe.Result
		err        error
		want       Outcome
		wantStatus facts.FactStatus
		wantValue  any
	}{
		{"value", probe.Result{Parsed: 3, Raw: "3"}, nil, Value, "", 3},
		{"not found", probe.Result{Status: probe.StatusNotFound}, nil, NotFound, facts.FactStatusNotFound, nil},
		{"forbidden", probe.Result{}, fmt.Errorf("%w: AccessDenied", probe.ErrForbidden), Forbidden, facts.FactStatusForbidden, nil},
		{"transient", probe.Result{}, fmt.Errorf("%w: timeout", probe.ErrTransient), Transient, facts.FactStatusTransient, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := facts.NewInMemory()
			got, err := Record(store, "db", "available", tc.res, tc.err)
			if err != nil || got != tc.want {
				t.Fatalf("Record = (%q, %v), want (%q, nil)", got, err, tc.want)
			}
			f := store.Latest("db", "available")
			if f == nil || f.Status != tc.wantStatus || f.Value != tc.wantValue || f.Collector != "probe" {
				t.Fatalf("recorded %+v, want status %q value %v from probe", f, tc.wantStatus, tc.wantValue)
			}
		})
	}
}

// An error outside the forbidden/transient taxonomy stops the caller and
// leaves nothing behind that the engine could misread.
func TestRecord_OtherErrorRecordsNothing(t *testing.T) {
	store := facts.NewInMemory()
	boom := fmt.Errorf("%w: bad flag", probe.ErrUsage)
	if _, err := Record(store, "db", "available", probe.Result{}, boom); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if store.FactsFor("db") != nil {
		t.Fatalf("recorded %+v, want nothing", store.FactsFor("db"))
	}
}

// installProvider writes a minimal provider into $MGTT_HOME, with a built
// runner binary when withBinary is set.
func installProvider(t *testing.T, home, name string, withBinary bool) *providersupport.Provider {
	t.Helper()
	dir := filepath.Join(home, "providers", name)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "meta:\n  name: " + name + "\n  version: 1.0.0\n  description: test\ninstall:\n  source:\n    build: b.sh\n    clean: c.sh\ntypes:\n  thing:\n    facts:\n      up:\n        type: mgtt.bool\n        probe:\n          cmd: \"echo true\"\n          parse: bool\n"
	files := map[string]string{"manifest.yaml": manifest, "b.sh": "#!/bin/sh\n", "c.sh": "#!/bin/sh\n"}
	if withBinary {
		files["bin/mgtt-provider-"+name] = "#!/bin/sh\necho '{\"value\": true}'\n"
	}
	for f, body := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p, err := providersupport.LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A provider gets a runner only once its binary exists; a types-only
// provider keeps running its probe.cmd through the shell.
func TestNew_RunnerOnlyForBuiltBinaries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MGTT_HOME", home)
	t.Setenv("MGTT_FIXTURES", "")
	reg := providersupport.NewRegistry()
	reg.Register(installProvider(t, home, "built", true))
	reg.Register(installProvider(t, home, "typesonly", false))

	d, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Runnable("built", "") {
		t.Error("built provider with no cmd should be runnable through its runner")
	}
	if d.Runnable("typesonly", "") {
		t.Error("types-only provider with no cmd can only be answered by an operator")
	}
	if !d.Runnable("typesonly", "echo true") {
		t.Error("a cmd is always runnable through the shell")
	}
}
