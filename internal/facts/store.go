// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package facts

// NewInMemory returns an empty in-memory fact store.
func NewInMemory() *Store {
	return &Store{facts: make(map[string][]Fact)}
}

// Append adds a fact to the component's list.
func (s *Store) Append(component string, f Fact) {
	s.facts[component] = append(s.facts[component], f)
}

// Latest returns the most recently appended fact for the given component and
// key, or nil if no such fact exists.
func (s *Store) Latest(component, key string) *Fact {
	list, ok := s.facts[component]
	if !ok {
		return nil
	}
	// Iterate in reverse: last appended is considered latest.
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Key == key {
			f := list[i]
			return &f
		}
	}
	return nil
}

// FactsFor returns all facts recorded for a component, in append order.
func (s *Store) FactsFor(component string) []Fact {
	list := s.facts[component]
	if len(list) == 0 {
		return nil
	}
	out := make([]Fact, len(list))
	copy(out, list)
	return out
}

// AllComponents returns the names of all components that have at least one fact.
func (s *Store) AllComponents() []string {
	out := make([]string, 0, len(s.facts))
	for k := range s.facts {
		out = append(out, k)
	}
	return out
}

// IsDiskBacked reports whether the store is backed by a file on disk.
func (s *Store) IsDiskBacked() bool {
	return s.path != ""
}

// LookupValue satisfies expr.FactLookup. Returns the latest value for
// (component, key) and whether such a fact exists.
//
// A stored fact with Value == nil is treated as "observed but unresolved"
// — this is the not_found path: the probe executed successfully, but the
// underlying resource is missing. The engine's expr layer converts the
// missing return into an UnresolvedError so state derivation records it
// and moves on to a different probe.
func (s *Store) LookupValue(component, key string) (any, bool) {
	f := s.Latest(component, key)
	if f == nil {
		return nil, false
	}
	if f.Value == nil {
		return nil, false
	}
	return f.Value, true
}

// PartialVisibility counts, across all components, the distinct facts the
// probe layer could not resolve because the provider was forbidden or the
// probe failed transiently (and which were never later resolved to a
// value). It reads the authoritative Fact.Status rather than parsing
// human-facing outcome strings, so the partial-visibility warning stays
// correct regardless of how an outcome is rendered (CLI, MCP, replay).
func (s *Store) PartialVisibility() (forbidden, transient int) {
	for _, list := range s.facts {
		// Last write wins per key (facts are appended in probe order).
		latest := make(map[string]FactStatus, len(list))
		for _, f := range list {
			latest[f.Key] = f.Status
		}
		for _, st := range latest {
			switch st {
			case FactStatusForbidden:
				forbidden++
			case FactStatusTransient:
				transient++
			}
		}
	}
	return forbidden, transient
}

// IsAbsent reports whether the probe layer has demonstrated the
// component doesn't exist: at least one fact recorded Status ==
// not_found AND no fact resolved to a real value. The second clause
// matters — a not_found on one sub-resource fact must not mask a sibling
// fact that DID resolve (e.g. a failing value), which would wrongly
// classify a present-but-broken component as absent and drop the
// scenario that explains it. The live-set filter uses this to eliminate
// scenarios requiring a non-default state on a genuinely-absent component.
func (s *Store) IsAbsent(component string) bool {
	list, ok := s.facts[component]
	if !ok {
		return false
	}
	sawNotFound := false
	for _, f := range list {
		if f.Value != nil {
			// A resolved value proves the component exists.
			return false
		}
		if f.Status == FactStatusNotFound {
			sawNotFound = true
		}
	}
	return sawNotFound
}
