package facts

import "time"

// Store is an in-memory append-only fact store.
type Store struct {
	facts map[string][]Fact
}

// Fact is a single observed value for a named key on a component.
type Fact struct {
	Key       string
	Value     any
	Collector string
	At        time.Time
	Note      string
	Raw       string
}
