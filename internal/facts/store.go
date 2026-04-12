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
