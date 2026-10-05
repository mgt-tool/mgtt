// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
	"github.com/mgt-tool/mgtt/internal/scenarios"
)

// Drafts are scenarios written from a model's own failure chains, for an
// author to review and keep: one with every component healthy, and one per
// root and root state.
type Drafts struct {
	Drafts []Draft
	// Chains is how many failure chains the model has.
	Chains int
	// Unshowable names failures, as component.state, for which no facts
	// were found that put the component in that state with its healthy
	// rules failing -- most often because the rules hold there, so a
	// component in that state reads as healthy.
	Unshowable []string
}

// Draft is one drafted scenario.
type Draft struct {
	Scenario *Scenario
	// Chain is the failure chain the facts were drawn from, zero for the
	// all-healthy draft; Count is how many chains share its root and root
	// state.
	Chain scenarios.Scenario
	Count int
	// Review is set when the conclusion is not the one the chain implies:
	// the engine names another root cause from these facts, and no
	// redundancy group explains it. Which is right is the author's call.
	Review string
}

// SuggestOptions narrows Suggest.
type SuggestOptions struct {
	// Component keeps only the drafts for failures rooted at it.
	Component string
}

// Suggest drafts scenarios from m's failure chains. For each root and root
// state it takes one chain -- the shortest that reaches the entry point, or
// the longest when none does -- and injects facts that put each of the
// chain's components in its state with its healthy rules failing, and every
// component the failure cannot reach in its default state with its rules
// holding. A component the failure can reach off that chain is left out,
// unknown, as it would be mid-incident. Where the entry point cannot reach
// the chain's end -- a background job no request path touches -- the draft
// starts diagnosis there instead (entry:). expect: is what the engine
// concludes from exactly those facts, so a draft passes as written: it is a
// baseline to review, not a verdict.
func Suggest(m *model.Model, reg *providersupport.Registry, opts SuggestOptions) (*Drafts, error) {
	if opts.Component != "" && m.Components[opts.Component] == nil {
		return nil, fmt.Errorf("component %q is not in the model", opts.Component)
	}
	all := scenarios.Enumerate(m, reg)
	out := &Drafts{Chains: len(all)}
	wit := &witnesses{m: m, reg: reg, vars: m.VarLookup(reg), found: map[witnessKey]witness{}}

	if opts.Component == "" {
		inject := map[string]map[string]any{}
		for _, name := range m.Order {
			if f, ok := wit.healthy(name); ok {
				inject[name] = f
			}
		}
		d := draft(m, reg, "all healthy", "Every component healthy: no root cause.", "", inject)
		if got := d.Scenario.Expect.RootCause; got != "none" {
			d.Review = fmt.Sprintf("a healthy system reads as broken: the engine names %s", got)
		}
		out.Drafts = append(out.Drafts, d)
	}

	entry := m.EntryPoint()
	seen := below(m, entry)
	for _, c := range classify(all, entry) {
		root := c.rep.Root
		if opts.Component != "" && root.Component != opts.Component {
			continue
		}
		inject := map[string]map[string]any{}
		for _, step := range c.rep.Chain {
			if f, ok := wit.failing(step.Component, step.State); ok {
				inject[step.Component] = f
			}
		}
		if inject[root.Component] == nil {
			out.Unshowable = append(out.Unshowable, root.Component+"."+root.State)
			continue
		}
		for _, name := range m.Order {
			if c.reach[name] {
				continue
			}
			if f, ok := wit.healthy(name); ok {
				inject[name] = f
			}
		}
		seenAt := ""
		if !seen[c.rep.Terminal()] {
			seenAt = c.rep.Terminal()
		}
		desc := fmt.Sprintf("From the failure chain %s, one of %d rooted at %s.%s.", RenderChain(c.rep), c.count, root.Component, root.State)
		d := draft(m, reg, root.Component+" "+strings.ReplaceAll(root.State, "_", " "), desc, seenAt, inject)
		d.Chain, d.Count = c.rep, c.count
		d.Review = review(root.Component, d.Scenario.Expect)
		out.Drafts = append(out.Drafts, d)
	}
	return out, nil
}

// RenderChain writes a chain as component.state steps joined by arrows.
func RenderChain(s scenarios.Scenario) string {
	parts := make([]string, len(s.Chain))
	for i, st := range s.Chain {
		parts[i] = st.Component + "." + st.State
	}
	return strings.Join(parts, " → ")
}

func draft(m *model.Model, reg *providersupport.Registry, name, desc, entry string, inject map[string]map[string]any) Draft {
	sc := &Scenario{Name: name, Description: desc, Entry: entry, Inject: inject}
	got := Run(m, reg, sc).Actual
	sc.Expect = Expectation{RootCause: got.RootCause, Path: got.Path, Eliminated: got.Eliminated, RedundancyDegraded: got.RedundancyDegraded}
	return Draft{Scenario: sc}
}

// class is the chains that share a root and root state.
type class struct {
	rep   scenarios.Scenario // the chain to draft from
	count int
	reach map[string]bool // every component on any of the chains
}

// review is the note a draft for a failure rooted at root carries when the
// engine concludes got: none when it names root, or root is a member of a
// redundancy group that holds.
func review(root string, got Expectation) string {
	if got.RootCause == root || slices.Contains(got.RedundancyDegraded, root) {
		return ""
	}
	return fmt.Sprintf("the engine names %s from these facts, not %s", got.RootCause, root)
}

// below is every component entry reaches through dependencies, itself
// included: where a diagnosis starting at entry can look.
func below(m *model.Model, entry string) map[string]bool {
	seen := map[string]bool{entry: true}
	for queue := []string{entry}; len(queue) > 0; queue = queue[1:] {
		for _, d := range m.DependenciesOf(queue[0]) {
			if !seen[d] {
				seen[d] = true
				queue = append(queue, d)
			}
		}
	}
	return seen
}

// classify groups chains by root and root state, in order of first
// appearance, choosing each class's chain to draft from.
func classify(all []scenarios.Scenario, entry string) []*class {
	type key struct{ component, state string }
	at := map[key]*class{}
	var out []*class
	for _, s := range all {
		k := key{s.Root.Component, s.Root.State}
		c := at[k]
		switch {
		case c == nil:
			c = &class{rep: s, reach: map[string]bool{}}
			at[k] = c
			out = append(out, c)
		case better(s, c.rep, entry):
			c.rep = s
		}
		c.count++
		for _, st := range s.Chain {
			c.reach[st.Component] = true
		}
	}
	return out
}

// better reports whether a is a better chain to draft from than b: one that
// ends at the entry point beats one that does not; among those that do, the
// shorter; among those that do not, the longer, which shows the failure
// furthest from its root.
func better(a, b scenarios.Scenario, entry string) bool {
	aEntry, bEntry := a.Terminal() == entry, b.Terminal() == entry
	switch {
	case aEntry != bEntry:
		return aEntry
	case aEntry:
		return len(a.Chain) < len(b.Chain)
	default:
		return len(a.Chain) > len(b.Chain)
	}
}

type witnessKey struct {
	component, state string
	healthy          bool
}

type witness struct {
	facts map[string]any
	ok    bool
}

// witnesses finds and remembers the facts that show a component in a state.
// The maps it returns are shared between drafts and must not be changed.
type witnesses struct {
	m     *model.Model
	reg   *providersupport.Registry
	vars  expr.VarLookup
	found map[witnessKey]witness
}

// failing is facts that put component in state with its healthy rules failing.
func (w *witnesses) failing(component, state string) (map[string]any, bool) {
	return w.find(witnessKey{component, state, false})
}

// healthy is facts that put component in its default state with its healthy
// rules holding.
func (w *witnesses) healthy(component string) (map[string]any, bool) {
	t := w.typeOf(component)
	if t == nil || t.DefaultActiveState == "" {
		return nil, false
	}
	return w.find(witnessKey{component, t.DefaultActiveState, true})
}

func (w *witnesses) find(k witnessKey) (map[string]any, bool) {
	if r, seen := w.found[k]; seen {
		return r.facts, r.ok
	}
	var r witness
	if t := w.typeOf(k.component); t != nil {
		rules := w.m.Components[k.component].HealthyRules(t)
		r.facts, r.ok = model.Witness(rules, t, k.component, w.vars, k.state, k.healthy)
	}
	w.found[k] = r
	return r.facts, r.ok
}

func (w *witnesses) typeOf(component string) *providersupport.Type {
	c := w.m.Components[component]
	if c == nil {
		return nil
	}
	t, _, err := c.ResolveType(w.m, w.reg)
	if err != nil {
		return nil
	}
	return t
}

// MarshalDraft writes d as a scenario file: comment lines naming what to
// review, then the scenario, each component's facts on one line, in order.
func MarshalDraft(d Draft, order []string) ([]byte, error) {
	sc := d.Scenario
	doc := &yaml.Node{Kind: yaml.MappingNode}
	add := func(m *yaml.Node, k string, v *yaml.Node) { m.Content = append(m.Content, str(k), v) }
	add(doc, "name", str(sc.Name))
	add(doc, "description", str(sc.Description))
	if sc.Entry != "" {
		add(doc, "entry", str(sc.Entry))
	}

	inject := &yaml.Node{Kind: yaml.MappingNode}
	for _, name := range order {
		facts, ok := sc.Inject[name]
		if !ok {
			continue
		}
		keys := make([]string, 0, len(facts))
		for k := range facts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		row := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
		for _, k := range keys {
			v := &yaml.Node{}
			if err := v.Encode(facts[k]); err != nil {
				return nil, fmt.Errorf("encode %s.%s: %w", name, k, err)
			}
			add(row, k, v)
		}
		add(inject, name, row)
	}
	add(doc, "inject", inject)

	expect := &yaml.Node{Kind: yaml.MappingNode}
	add(expect, "root_cause", str(sc.Expect.RootCause))
	for _, l := range []struct {
		key  string
		list []string
	}{{"path", sc.Expect.Path}, {"eliminated", sc.Expect.Eliminated}, {"redundancy_degraded", sc.Expect.RedundancyDegraded}} {
		if len(l.list) == 0 {
			continue
		}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, x := range l.list {
			seq.Content = append(seq.Content, str(x))
		}
		add(expect, l.key, seq)
	}
	add(doc, "expect", expect)

	doc.HeadComment = "Drafted by mgtt: expect: is what the engine concludes from these facts today.\nCheck the facts and the conclusion before keeping it."
	if d.Review != "" {
		doc.HeadComment += "\nREVIEW: " + d.Review + "."
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode draft: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode draft: %w", err)
	}
	return buf.Bytes(), nil
}

func str(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }

// WriteDraft saves d into dir as a file named after the scenario, never over
// one that is there (a *ScenarioExistsError then), and returns its path.
func WriteDraft(dir string, d Draft, order []string) (string, error) {
	data, err := MarshalDraft(d, order)
	if err != nil {
		return "", err
	}
	var name strings.Builder
	for _, r := range strings.ToLower(d.Scenario.Name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			name.WriteRune(r)
		default:
			name.WriteRune('-')
		}
	}
	path := filepath.Join(dir, name.String()+".yaml")
	return path, writeNew(path, data)
}
