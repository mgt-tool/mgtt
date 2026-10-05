// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package validate runs correctness checks on a loaded provider.
//
// Static checks (always safe in CI):
//   - meta fields populated
//   - every fact has a probe.cmd, unless a runner binary serves the probes
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
	"github.com/mgt-tool/mgtt/internal/model"
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
		checkType(typeName, typ, p.Variables, hasRunner(p), r)
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
func checkType(typeName string, typ *providersupport.Type, vars map[string]providersupport.Variable, runner bool, r *Report) {
	declaredStates := make(map[string]bool, len(typ.States))
	for _, s := range typ.States {
		declaredStates[s.Name] = true
	}
	declaredFacts := make(map[string]string, len(typ.Facts))
	for name, f := range typ.Facts {
		declaredFacts[name] = f.TypeName
	}
	refs := exprRefs{facts: declaredFacts, vars: vars}
	checkDefaultActiveState(typeName, typ, declaredStates, r)
	checkFailureModeStates(typeName, typ, declaredStates, r)
	checkHealthyFactRefs(typeName, typ, refs, r)
	checkStateWhenFactRefs(typeName, typ, refs, r)
	checkFactProbes(typeName, typ, runner, r)
	checkHealthMatchesStates(typeName, typ, r)
}

// checkHealthMatchesStates warns when the type's healthy rules and its
// states disagree for some facts: healthy outside the default state, or
// unhealthy in it. Every model using the type inherits the disagreement,
// and simulate (states) and diagnose (rules) read those facts
// differently.
func checkHealthMatchesStates(typeName string, typ *providersupport.Type, r *Report) {
	for _, d := range model.HealthStateDisagreements(typ.Healthy, typ, "x", nil) {
		if d.State == "" {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s: healthy rules fail where no state matches, so the failure has no state (e.g. %s)", typeName, d.WitnessString()))
		} else if d.Healthy {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s: healthy rules hold in state %q, which is not the default state %q (e.g. %s)", typeName, d.State, typ.DefaultActiveState, d.WitnessString()))
		} else {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s: healthy rules fail in the default state %q (e.g. %s)", typeName, d.State, d.WitnessString()))
		}
	}
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

// exprRefs is what a type's expressions may name: its declared facts, and,
// as the right-hand side of a comparison, its provider's declared variables.
type exprRefs struct {
	facts map[string]string // fact name → type name
	vars  map[string]providersupport.Variable
}

// healthy and state.when may reference only declared facts on this type,
// or a declared variable as the value compared against.
func checkHealthyFactRefs(typeName string, typ *providersupport.Type, refs exprRefs, r *Report) {
	for i, h := range typ.Healthy {
		for _, ref := range referencedFacts(h, refs) {
			if _, ok := refs.facts[ref]; !ok {
				r.Failures = append(r.Failures, fmt.Sprintf(
					"%s: healthy[%d] references %q, which is neither a declared fact nor a declared variable", typeName, i, ref))
			}
		}
	}
}

func checkStateWhenFactRefs(typeName string, typ *providersupport.Type, refs exprRefs, r *Report) {
	for _, s := range typ.States {
		if s.When == nil {
			continue
		}
		for _, ref := range referencedFacts(s.When, refs) {
			if _, ok := refs.facts[ref]; !ok {
				r.Failures = append(r.Failures, fmt.Sprintf(
					"%s: state %q references %q, which is neither a declared fact nor a declared variable", typeName, s.Name, ref))
			}
		}
	}
}

// hasRunner reports whether probes go to a provider binary, which dispatches
// on type and fact and never reads probe.cmd: an explicit entrypoint, or the
// bin/mgtt-provider-<name> that every source or image install provides.
func hasRunner(p *providersupport.Provider) bool {
	return p.Runtime.Entrypoint != "" || p.Install.Source != nil || p.Install.Image != nil
}

func checkFactProbes(typeName string, typ *providersupport.Type, runner bool, r *Report) {
	for factName, f := range typ.Facts {
		if f.Probe.Cmd == "" && !runner {
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
//
// A bare word on the right-hand side is read the way the evaluator reads
// it: against a string fact, a literal ("phase == Bound"); against any other
// fact, a second fact ("ready_replicas < desired_replicas") or, when the
// provider declares it under variables:, a per-component variable
// ("restart_count <= max_restart_count"). Only facts are returned.
func referencedFacts(n expr.Node, refs exprRefs) []string {
	var out []string
	walk(n, refs, &out)
	return out
}

func walk(n expr.Node, refs exprRefs, out *[]string) {
	switch v := n.(type) {
	case *expr.AndNode:
		walk(v.L, refs, out)
		walk(v.R, refs, out)
	case *expr.OrNode:
		walk(v.L, refs, out)
		walk(v.R, refs, out)
	case *expr.CmpNode:
		if v.Component != "" || v.Fact == "" || v.Fact == "state" {
			return
		}
		*out = append(*out, v.Fact)
		s, ok := v.Value.(string)
		if !ok || !isIdentifier(s) || refs.facts[v.Fact] == stringFactType {
			return
		}
		if _, isFact := refs.facts[s]; !isFact {
			if _, isVar := refs.vars[s]; isVar {
				return
			}
		}
		*out = append(*out, s)
	}
}

// stringFactType is the fact type whose comparisons take bare-word literals.
const stringFactType = "mgtt.string"

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
