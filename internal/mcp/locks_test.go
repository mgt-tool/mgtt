package mcp

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestProbeTimeout_ClampedTo5Minutes — operator-configurable probe
// timeout max is 5m per design §7.2. A typo of --probe-timeout=3000
// (seconds) must not silently bind the server for ~50 minutes.
func TestProbeTimeout_ClampedTo5Minutes(t *testing.T) {
	got := probeTimeoutFromConfig(Config{ProbeTimeoutSeconds: 3000})
	want := 300 * time.Second
	if got != want {
		t.Errorf("got %v, want %v (clamped to 5m)", got, want)
	}
}

func TestProbeTimeout_UnderCapPassesThrough(t *testing.T) {
	got := probeTimeoutFromConfig(Config{ProbeTimeoutSeconds: 60})
	want := 60 * time.Second
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestProbeTimeout_ZeroMeansRunnerDefault(t *testing.T) {
	got := probeTimeoutFromConfig(Config{ProbeTimeoutSeconds: 0})
	if got != 0 {
		t.Errorf("got %v, want 0 (runner default sentinel)", got)
	}
}

// TestLockFor_SerializesConcurrentFactAdds asserts the per-incident
// mutex from locks.go actually serialises writers. Without the lock,
// concurrent FactAdds race on facts.Store's map/slice and the final
// fact count is non-deterministic (race detector catches it outright).
// With the lock, all N appends land.
func TestLockFor_SerializesConcurrentFactAdds(t *testing.T) {
	h, incidentID := startFixtureIncident(t)

	const N = 50
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = h.FactAdd(FactAddParams{
				IncidentID: incidentID,
				Component:  "api",
				Key:        fmt.Sprintf("k%d", i),
				Value:      i,
			})
		}(i)
	}
	wg.Wait()

	list, err := h.FactsList(FactsListParams{IncidentID: incidentID})
	if err != nil {
		t.Fatalf("FactsList: %v", err)
	}
	if len(list.Facts) != N {
		t.Errorf("concurrent appends lost data: got %d want %d", len(list.Facts), N)
	}
}

// TestLockFor_ReturnsSameMutexForSameID is a white-box check that the
// registry reuses the same RWMutex for repeated calls on the same id.
// Otherwise the serialization promise is a lie.
func TestLockFor_ReturnsSameMutexForSameID(t *testing.T) {
	a := lockFor("test-lock-same")
	b := lockFor("test-lock-same")
	if a != b {
		t.Error("lockFor must return the same RWMutex for the same id")
	}
}

func TestLockFor_DifferentIDsGetDifferentMutexes(t *testing.T) {
	a := lockFor("test-lock-A")
	b := lockFor("test-lock-B")
	if a == b {
		t.Error("different incident ids must get independent mutexes")
	}
}
