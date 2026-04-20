package build

import (
	"errors"
	"strings"
	"testing"
)

func TestGateDeletions_NoDeletions(t *testing.T) {
	d := Diff{Added: []string{"new-component"}}
	if err := GateDeletions(d, GateFlags{}); err != nil {
		t.Errorf("unexpected gate failure: %v", err)
	}
}

func TestGateDeletions_BlocksByDefault(t *testing.T) {
	d := Diff{Removed: []string{"old-api", "legacy-rds"}}
	err := GateDeletions(d, GateFlags{})
	if err == nil {
		t.Fatal("expected gate to refuse deletion")
	}
	msg := err.Error()
	if !strings.Contains(msg, "old-api") || !strings.Contains(msg, "legacy-rds") {
		t.Errorf("error should name the components; got: %s", msg)
	}
	if !errors.Is(err, ErrDeletionsRefused) {
		t.Errorf("expected ErrDeletionsRefused; got: %v", err)
	}
}

func TestGateDeletions_AllowDeletes(t *testing.T) {
	d := Diff{Removed: []string{"old-api"}}
	if err := GateDeletions(d, GateFlags{AllowDeletes: true}); err != nil {
		t.Errorf("AllowDeletes should pass; got: %v", err)
	}
}

func TestGateDeletions_TombstoneProtects(t *testing.T) {
	d := Diff{Removed: []string{"air-gapped-db", "truly-gone"}}
	err := GateDeletions(d, GateFlags{Tombstone: []string{"air-gapped-db"}})
	if err == nil {
		t.Fatal("non-tombstoned removal must still fail")
	}
	if !strings.Contains(err.Error(), "truly-gone") {
		t.Errorf("error should name the unprotected removal; got: %s", err)
	}
	if strings.Contains(err.Error(), "air-gapped-db") {
		t.Errorf("tombstoned component should NOT appear in the error; got: %s", err)
	}
}

func TestGateDeletions_TombstoneFullyCovers(t *testing.T) {
	d := Diff{Removed: []string{"air-gapped-db"}}
	if err := GateDeletions(d, GateFlags{Tombstone: []string{"air-gapped-db"}}); err != nil {
		t.Errorf("fully-tombstoned removals must pass; got: %v", err)
	}
}

// A Tombstone entry for a component that isn't being removed must be
// harmless — silently ignored, doesn't accidentally block or count.
// Guards against a future refactor that might try to validate
// tombstone names against the known model.
func TestGateDeletions_TombstoneIgnoresUnknownName(t *testing.T) {
	d := Diff{Removed: []string{"a"}}
	err := GateDeletions(d, GateFlags{Tombstone: []string{"a", "never-existed"}})
	if err != nil {
		t.Errorf("unknown tombstone name should be silently ignored; got: %v", err)
	}
}
