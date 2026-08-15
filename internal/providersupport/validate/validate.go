// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package validate runs correctness checks on a loaded provider.
//
// Static checks (always safe in CI):
//   - meta fields populated
//   - every fact has a probe.cmd
//   - default_active_state references a declared state
//   - auth.access.writes is "none" (or warn if any other value)
//   - meta.requires.mgtt is satisfied
//
// Live checks (require backend access; opt-in via --live in the CLI) are
// orchestrated separately and not part of this package.
package validate

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
)

// Report holds the outcome of validation. OK reports whether any failures
// were recorded; warnings do not affect OK.
type Report struct {
	Passed   []string
	Warnings []string
	Failures []string
}

func (r Report) OK() bool { return len(r.Failures) == 0 }

// Static runs all checks that do not touch the backend.
func Static(p *providersupport.Provider) Report {
	r := &Report{}
	staticInto(p, r)
	return *r
}

func staticInto(p *providersupport.Provider, r *Report) {
	checkMeta(p, r)
	checkWritePosture(p, r)
	if err := p.CheckCompatible(); err != nil {
		r.Failures = append(r.Failures, err.Error())
	}
	checkAbsoluteEntrypoint(p.Runtime.Entrypoint, r)
	for typeName, typ := range p.Types {
		checkType(typeName, typ, r)
	}
	checkNeedsVocabulary(p.Runtime.Needs, r)
	if r.OK() && len(r.Warnings) == 0 {
		r.Passed = append(r.Passed, "static checks: ok")
	}
}

// checkMeta flags missing meta.name / meta.version. v1.0 removed
// meta.command (entrypoint lives in runtime.entrypoint).
func checkMeta(p *providersupport.Provider, r *Report) {
	if p.Meta.Name == "" {
		r.Failures = append(r.Failures, "meta.name is empty")
	}
	if p.Meta.Version == "" {
		r.Failures = append(r.Failures, "meta.version is empty")
	}
}

// checkWritePosture enforces the read_only / writes_note contract.
// read_only: false is valid only with a non-empty writes_note that
// the CLI surfaces for operator consent at install time.
func checkWritePosture(p *providersupport.Provider, r *Report) {
	if p.ReadOnly {
		return
	}
	if strings.TrimSpace(p.WritesNote) == "" {
		r.Failures = append(r.Failures,
			"read_only: false requires writes_note: describing the side effect")
		return
	}
	r.Warnings = append(r.Warnings,
		"read_only: false — operators must confirm credentials match the declared writes")
}

// checkAbsoluteEntrypoint verifies the declared entrypoint path exists
// when authored as an absolute filesystem path. Empty entrypoints fall
// back to the convention (providerDir/bin/mgtt-provider-<name>) and
// can't be checked without the install directory — skip them.
func checkAbsoluteEntrypoint(cmd string, r *Report) {
	if cmd == "" || cmd[0] != '/' {
		return
	}
	if _, err := os.Stat(cmd); os.IsNotExist(err) {
		r.Failures = append(r.Failures, fmt.Sprintf(
			"runtime.entrypoint %q does not exist on disk", cmd))
	}
}

// checkNeedsVocabulary rejects runtime.needs entries that don't
// resolve against the known image-cap vocabulary (built-ins +
// operator overrides). Sorted iteration so failure lines are stable.
func checkNeedsVocabulary(needs map[string]string, r *Report) {
	if len(needs) == 0 {
		return
	}
	names := make([]string, 0, len(needs))
	for n := range needs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if probe.Known(n) {
			continue
		}
		r.Failures = append(r.Failures, fmt.Sprintf(
			"unknown capability %q (known: %s); add it to $MGTT_HOME/capabilities.yaml or remove from needs",
			n, strings.Join(probe.KnownNames(), ", ")))
	}
}

// checkType validates one type's default state + failure-mode + healthy
// + state-predicate + fact-probe rules. Split out of Static so the per-
// type concerns don't overwhelm the shared preamble.
func checkType(typeName string, typ *providersupport.Type, r *Report) {
	declaredStates := make(map[string]bool, len(typ.States))
	for _, s := range typ.States {
		declaredStates[s.Name] = true
	}
	declaredFacts := make(map[string]bool, len(typ.Facts))
	for f := range typ.Facts {
		declaredFacts[f] = true
	}
	checkDefaultActiveState(typeName, typ, declaredStates, r)
	checkFailureModeStates(typeName, typ, declaredStates, r)
	checkHealthyFactRefs(typeName, typ, declaredFacts, r)
	checkStateWhenFactRefs(typeName, typ, declaredFacts, r)
	checkFactProbes(typeName, typ, r)
}

func checkDefaultActiveState(typeName string, typ *providersupport.Type, declaredStates map[string]bool, r *Report) {
	if typ.DefaultActiveState != "" && !declaredStates[typ.DefaultActiveState] {
		r.Failures = append(r.Failures, fmt.Sprintf(
			"%s: default_active_state %q is not in declared states",
			typeName, typ.DefaultActiveState))
	}
}

func checkFailureModeStates(typeName string, typ *providersupport.Type, declaredStates map[string]bool, r *Report) {
	for stateName := range typ.FailureModes {
		if !declaredStates[stateName] {
			r.Failures = append(r.Failures, fmt.Sprintf(
				"%s: failure_modes references undeclared state %q",
				typeName, stateName))
		}
	}
}

// healthy and state.when may reference only declared facts on this type.
func checkHealthyFactRefs(typeName string, typ *providersupport.Type, declaredFacts map[string]bool, r *Report) {
	for i, h := range typ.Healthy {
		for _, ref := range referencedFacts(h) {
			if !declaredFacts[ref] {
				r.Failures = append(r.Failures, fmt.Sprintf(
					"%s: healthy[%d] references undeclared fact %q", typeName, i, ref))
			}
		}
	}
}

func checkStateWhenFactRefs(typeName string, typ *providersupport.Type, declaredFacts map[string]bool, r *Report) {
	for _, s := range typ.States {
		if s.When == nil {
			continue
		}
		for _, ref := range referencedFacts(s.When) {
			if !declaredFacts[ref] {
				r.Failures = append(r.Failures, fmt.Sprintf(
					"%s: state %q references undeclared fact %q", typeName, s.Name, ref))
			}
		}
	}
}

func checkFactProbes(typeName string, typ *providersupport.Type, r *Report) {
	for factName, f := range typ.Facts {
		if f.Probe.Cmd == "" {
			r.Failures = append(r.Failures, fmt.Sprintf(
				"%s/%s: probe.cmd is empty", typeName, factName))
		}
		if f.Probe.Parse == "" {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"%s/%s: probe.parse empty (defaults to string)", typeName, factName))
		}
	}
}

// referencedFacts walks an expr.Node and returns every fact name it reads.
// "state" is ignored — that's a reserved pseudo-fact resolved from the
// evaluation context. Cross-component references (CmpNode.Component != "")
// are also ignored because cross-component validation is a model concern,
// not a provider concern.
func referencedFacts(n expr.Node) []string {
	var out []string
	walk(n, &out)
	return out
}

func walk(n expr.Node, out *[]string) {
	switch v := n.(type) {
	case *expr.AndNode:
		walk(v.L, out)
		walk(v.R, out)
	case *expr.OrNode:
		walk(v.L, out)
		walk(v.R, out)
	case *expr.CmpNode:
		if v.Component != "" || v.Fact == "" || v.Fact == "state" {
			return
		}
		*out = append(*out, v.Fact)
		// If the RHS is a bare identifier, it's a fact reference too
		// (e.g. "ready_replicas < desired_replicas").
		if s, ok := v.Value.(string); ok && isIdentifier(s) {
			*out = append(*out, s)
		}
	}
}

func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c == '_':
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
