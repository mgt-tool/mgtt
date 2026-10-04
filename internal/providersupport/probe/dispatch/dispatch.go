// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package dispatch is the one way a probe is run and its outcome recorded.
// `mgtt plan`, `mgtt diagnose` and the MCP probe tool all go through it, so
// a fact is collected the same way, and lands in the store the same way,
// whichever surface asked for it.
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
	probeexec "github.com/mgt-tool/mgtt/internal/providersupport/probe/exec"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe/fixture"
)

// Dispatcher routes each probe to the provider's runner when it has one
// and to the shell executor otherwise. Under MGTT_FIXTURES every probe is
// answered from the fixture file instead.
type Dispatcher struct {
	exec    probe.Executor
	runners map[string]bool
	fixture bool
}

// New builds the dispatcher for the providers in reg.
func New(reg *providersupport.Registry) (*Dispatcher, error) {
	if path := os.Getenv("MGTT_FIXTURES"); path != "" {
		ex, err := fixture.Load(path)
		if err != nil {
			return nil, fmt.Errorf("load fixtures: %w", err)
		}
		return &Dispatcher{exec: ex, fixture: true}, nil
	}
	runners := map[string]probe.Executor{}
	for _, p := range reg.All() {
		if r := runnerFor(p); r != nil {
			runners[p.Meta.Name] = r
		}
	}
	d := &Dispatcher{exec: probeexec.Default(), runners: map[string]bool{}}
	if len(runners) > 0 {
		d.exec = &probe.Mux{Default: probeexec.Default(), Runners: runners}
		for name := range runners {
			d.runners[name] = true
		}
	}
	return d, nil
}

// runnerFor returns the executor for p's own binary or image, or nil when
// p has neither and its facts run as shell commands.
func runnerFor(p *providersupport.Provider) probe.Executor {
	dir := providersupport.ProviderDir(p.Meta.Name)
	meta, _ := providersupport.ReadInstallMeta(dir) // absent file → Method:git (backward-compat)
	if meta.Method == providersupport.InstallMethodImage {
		return probe.NewImageRunner(meta.Source, slices.Sorted(maps.Keys(p.Runtime.Needs)), p.Runtime.NetworkMode)
	}
	// A declared entrypoint is the author's word; the conventional
	// bin/mgtt-provider-<name> counts only once it has been built, so a
	// types-only provider keeps running its probe.cmd.
	if p.Runtime.Entrypoint != "" {
		return probe.NewExternalRunner(expandProviderDir(p.Runtime.Entrypoint, dir, p.Meta.Name))
	}
	if dir == "" {
		return nil
	}
	bin := p.ResolveEntrypoint(providersupport.InstallMethodGit, dir)
	if _, err := os.Stat(bin); err != nil {
		return nil
	}
	return probe.NewExternalRunner(bin)
}

// expandProviderDir substitutes $MGTT_PROVIDER_DIR in a declared entrypoint.
func expandProviderDir(command, dir, name string) string {
	if dir == "" {
		dir = filepath.Join("providers", name)
	}
	return strings.ReplaceAll(command, "$MGTT_PROVIDER_DIR", dir)
}

// Run executes cmd on the executor its provider routes to.
func (d *Dispatcher) Run(ctx context.Context, cmd probe.Command) (probe.Result, error) {
	return d.exec.Run(ctx, cmd)
}

// Runnable reports whether a probe can run at all: it has a command, its
// provider has a runner that needs none, or fixtures answer everything.
// When it is false the fact can only come from an operator.
func (d *Dispatcher) Runnable(provider, command string) bool {
	return command != "" || d.fixture || d.runners[provider]
}

// Outcome classifies what one probe run established.
type Outcome string

const (
	Value     Outcome = "value"     // the fact resolved
	NotFound  Outcome = "not_found" // the probe ran; the resource does not exist
	Forbidden Outcome = "forbidden" // the backend refused the credentials
	Transient Outcome = "transient" // a retryable failure, e.g. a timeout
)

// Record appends what a probe run established to store and classifies it.
// A value is recorded as such; not_found, forbidden and transient are
// recorded as value-less facts carrying that status, which the engine reads
// as "unknown" and never as healthy. Any other error is returned and
// nothing is recorded.
func Record(store *facts.Store, component, fact string, res probe.Result, runErr error) (Outcome, error) {
	f := facts.Fact{Key: fact, Collector: "probe", At: time.Now().UTC()}
	var outcome Outcome
	switch {
	case errors.Is(runErr, probe.ErrForbidden):
		outcome, f.Status, f.Note = Forbidden, facts.FactStatusForbidden, "forbidden: "+runErr.Error()
	case errors.Is(runErr, probe.ErrTransient):
		outcome, f.Status, f.Note = Transient, facts.FactStatusTransient, "transient: "+runErr.Error()
	case runErr != nil:
		return "", runErr
	case res.Status == probe.StatusNotFound:
		outcome, f.Status, f.Raw = NotFound, facts.FactStatusNotFound, res.Raw
	default:
		outcome, f.Value, f.Raw = Value, res.Parsed, res.Raw
	}
	store.Append(component, f)
	return outcome, nil
}
