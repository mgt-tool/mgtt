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
	var r Report

	if p.Meta.Name == "" {
		r.Failures = append(r.Failures, "meta.name is empty")
	}
	if p.Meta.Version == "" {
		r.Failures = append(r.Failures, "meta.version is empty")
	}
	if p.Meta.Command == "" {
		r.Failures = append(r.Failures, "meta.command is empty")
	}

	// Write-posture contract: read_only defaults to true. When a provider
	// declares read_only: false, writes_note must describe the side effect
	// so `mgtt provider install` can surface it for operator consent.
	if !p.ReadOnly {
		if strings.TrimSpace(p.WritesNote) == "" {
			r.Failures = append(r.Failures,
				"read_only: false requires writes_note: describing the side effect")
		} else {
			r.Warnings = append(r.Warnings,
				"read_only: false — operators must confirm credentials match the declared writes")
		}
	}

	if err := p.CheckCompatible(); err != nil {
		r.Failures = append(r.Failures, err.Error())
	}

	// meta.command: if set to a path (not a $VAR template), verify it exists.
	// Templates like "$MGTT_PROVIDER_DIR/bin/foo" are resolved at install time;
	// absolute paths can be checked here.
	if cmd := p.Meta.Command; cmd != "" && cmd[0] == '/' {
		if _, err := os.Stat(cmd); os.IsNotExist(err) {
			r.Failures = append(r.Failures, fmt.Sprintf(
				"meta.command %q does not exist on disk", cmd))
		}
	}

	for typeName, typ := range p.Types {
		declaredStates := make(map[string]bool, len(typ.States))
		for _, s := range typ.States {
			declaredStates[s.Name] = true
		}
		declaredFacts := make(map[string]bool, len(typ.Facts))
		for f := range typ.Facts {
			declaredFacts[f] = true
		}

		if typ.DefaultActiveState != "" && !declaredStates[typ.DefaultActiveState] {
			r.Failures = append(r.Failures, fmt.Sprintf(
				"%s: default_active_state %q is not in declared states",
				typeName, typ.DefaultActiveState))
		}

		// failure_modes references must all match declared states.
		for stateName := range typ.FailureModes {
			if !declaredStates[stateName] {
				r.Failures = append(r.Failures, fmt.Sprintf(
					"%s: failure_modes references undeclared state %q",
					typeName, stateName))
			}
		}

		// healthy: and state.when: expressions must reference declared facts
		// (same component — cross-component refs are model concerns).
		for i, h := range typ.Healthy {
			for _, factRef := range referencedFacts(h) {
				if !declaredFacts[factRef] {
					r.Failures = append(r.Failures, fmt.Sprintf(
						"%s: healthy[%d] references undeclared fact %q",
						typeName, i, factRef))
				}
			}
		}
		for _, s := range typ.States {
			if s.When == nil {
				continue
			}
			for _, factRef := range referencedFacts(s.When) {
				if !declaredFacts[factRef] {
					r.Failures = append(r.Failures, fmt.Sprintf(
						"%s: state %q references undeclared fact %q",
						typeName, s.Name, factRef))
				}
			}
		}

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

	// needs: every declared capability must resolve against the merged
	// vocabulary (built-ins + operator overrides). Shell-fallback providers
	// (no meta.command) cannot declare needs — they have no binary, which
	// means there's no image install target and no process whose environment
	// mgtt would forward anything into.
	if len(p.Needs) > 0 {
		if p.Meta.Command == "" {
			r.Failures = append(r.Failures,
				"needs declared but provider has no command (shell-fallback providers don't support image install)")
		}
		for _, n := range p.Needs {
			if !probe.Known(n) {
				r.Failures = append(r.Failures, fmt.Sprintf(
					"unknown capability %q (known: %s); add it to $MGTT_HOME/capabilities.yaml or remove from needs",
					n, joinNames(probe.KnownNames())))
			}
		}
	}

	// network: docker's three built-in modes. "" is acceptable (defaults
	// to bridge). Anything else is almost certainly a typo the user wants
	// to see now, not at probe time.
	switch p.Network {
	case "", "bridge", "host", "none":
		// ok
	default:
		r.Failures = append(r.Failures, fmt.Sprintf(
			"unknown network mode %q (valid: bridge, host, none)", p.Network))
	}

	if r.OK() && len(r.Warnings) == 0 {
		r.Passed = append(r.Passed, "static checks: ok")
	}
	return r
}

// joinNames is a tiny wrapper that avoids importing strings twice when the
// surrounding file doesn't already use it.
func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
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
