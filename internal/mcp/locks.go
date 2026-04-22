package mcp

import "sync"

// Per-incident lock registry. Design §8.2 mandates "one writer per
// incident at a time via sync.Mutex keyed by incident_id" — HTTP
// transport dispatches each request in its own goroutine, so two
// concurrent fact.add or probe calls on the same incident would race
// on facts.Store's maps without this serialization.
//
// Readers take RLock (Plan, FactsList, Scenarios*, IncidentSnapshot);
// writers take Lock (FactAdd, Probe when executing, IncidentEnd).
// incidentLocks itself is guarded by its own Mutex so first-use
// creation of a per-id RWMutex is race-free.
var (
	incidentLocksMu sync.Mutex
	incidentLocks   = map[string]*sync.RWMutex{}
)

// lockFor returns the RWMutex for an incident id, creating it on first
// use. Locks are never evicted: a long-running server may accumulate one
// mutex per incident id it's ever seen. A typical deployment handles
// thousands of incidents per day; the memory footprint (a few dozen
// bytes per mutex) is not worth a GC pass.
func lockFor(id string) *sync.RWMutex {
	incidentLocksMu.Lock()
	defer incidentLocksMu.Unlock()
	mu, ok := incidentLocks[id]
	if !ok {
		mu = &sync.RWMutex{}
		incidentLocks[id] = mu
	}
	return mu
}
