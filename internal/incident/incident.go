package incident

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sajonaro/mgtt/internal/facts"
)

// Incident represents an active or completed incident session.
type Incident struct {
	ID        string
	Model     string
	Version   string
	Started   time.Time
	Ended     time.Time // zero until End is called
	StateFile string
	Store     *facts.Store
}

const currentFile = ".mgtt-current"

// Start creates a new incident. If id is empty, one is generated.
// Returns an error if an incident is already active.
func Start(modelName, modelVersion, id string) (*Incident, error) {
	if id == "" {
		id = generateID()
	}

	// Check if incident already active
	if _, err := Current(); err == nil {
		return nil, fmt.Errorf("incident already in progress — run 'mgtt incident end' first")
	}

	now := time.Now()
	stateFile := id + ".state.yaml"

	meta := facts.StoreMeta{
		Model:    modelName,
		Version:  modelVersion,
		Incident: id,
		Started:  now,
	}
	store := facts.NewDiskBacked(stateFile, meta)
	if err := store.Save(); err != nil {
		return nil, fmt.Errorf("creating state file: %w", err)
	}

	// Write .mgtt-current pointer
	if err := os.WriteFile(currentFile, []byte(stateFile+"\n"), 0644); err != nil {
		return nil, fmt.Errorf("writing current pointer: %w", err)
	}

	return &Incident{
		ID:        id,
		Model:     modelName,
		Version:   modelVersion,
		Started:   now,
		StateFile: stateFile,
		Store:     store,
	}, nil
}

// End closes the current incident by removing the .mgtt-current pointer.
func End() (*Incident, error) {
	inc, err := Current()
	if err != nil {
		return nil, fmt.Errorf("no active incident: %w", err)
	}
	inc.Ended = time.Now()
	os.Remove(currentFile)
	return inc, nil
}

// Current reads .mgtt-current and loads the incident state.
func Current() (*Incident, error) {
	data, err := os.ReadFile(currentFile)
	if err != nil {
		return nil, fmt.Errorf("no active incident")
	}
	stateFile := strings.TrimSpace(string(data))
	store, err := facts.Load(stateFile)
	if err != nil {
		return nil, fmt.Errorf("loading state: %w", err)
	}
	return &Incident{
		ID:        store.Meta.Incident,
		Model:     store.Meta.Model,
		Version:   store.Meta.Version,
		Started:   store.Meta.Started,
		StateFile: stateFile,
		Store:     store,
	}, nil
}

// generateID creates an incident ID based on the current UTC time.
func generateID() string {
	now := time.Now().UTC()
	return fmt.Sprintf("inc-%s-%s-001", now.Format("20060102"), now.Format("1504"))
}
