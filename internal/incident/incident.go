package incident

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mgt-tool/mgtt/internal/facts"
)

// Incident represents an active or completed incident session.
type Incident struct {
	ID        string
	Model     string
	Version   string
	Started   time.Time
	Ended     time.Time // zero until End is called
	Verdict   string
	ModelRef  string // absolute path, empty for CLI-started
	StateFile string
	Store     *facts.Store
}

const currentFile = ".mgtt-current"

// Start creates a new incident for the CLI path: refuses if another is
// active (single-active invariant) and writes the `.mgtt-current` pointer
// so subsequent CLI commands see it.
func Start(modelName, modelVersion, id string) (*Incident, error) {
	if _, err := Current(); err == nil {
		return nil, fmt.Errorf("incident already in progress — run 'mgtt incident end' first")
	}
	inc, err := StartIsolated(modelName, modelVersion, id, "")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(currentFile, []byte(inc.StateFile+"\n"), 0644); err != nil {
		return nil, fmt.Errorf("writing current pointer: %w", err)
	}
	return inc, nil
}

// StartIsolated creates a new incident WITHOUT writing `.mgtt-current` and
// WITHOUT checking for another active incident. Used by the MCP path where
// agents may drive multiple concurrent incidents in one `$MGTT_HOME`.
// modelRef, when non-empty, is persisted so downstream tools can re-load
// the model without the caller passing the path again.
func StartIsolated(modelName, modelVersion, id, modelRef string) (*Incident, error) {
	if id == "" {
		id = generateID()
	}
	now := time.Now()
	stateFile := id + ".state.yaml"

	meta := facts.StoreMeta{
		Model:    modelName,
		Version:  modelVersion,
		Incident: id,
		Started:  now,
		ModelRef: modelRef,
	}
	store := facts.NewDiskBacked(stateFile, meta)
	if err := store.Save(); err != nil {
		return nil, fmt.Errorf("creating state file: %w", err)
	}
	return &Incident{
		ID:        id,
		Model:     modelName,
		Version:   modelVersion,
		Started:   now,
		ModelRef:  modelRef,
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

// LoadByID loads an incident state file by its id without consulting
// `.mgtt-current`. Used by the MCP path where agents hold incident IDs
// explicitly and multiple incidents coexist.
func LoadByID(id string) (*Incident, error) {
	stateFile := id + ".state.yaml"
	store, err := facts.Load(stateFile)
	if err != nil {
		return nil, fmt.Errorf("loading state for %q: %w", id, err)
	}
	return &Incident{
		ID:        store.Meta.Incident,
		Model:     store.Meta.Model,
		Version:   store.Meta.Version,
		Started:   store.Meta.Started,
		Ended:     store.Meta.Ended,
		Verdict:   store.Meta.Verdict,
		ModelRef:  store.Meta.ModelRef,
		StateFile: stateFile,
		Store:     store,
	}, nil
}

// EndByID marks the incident as ended (persisting the timestamp and
// optional verdict to the state file) without touching `.mgtt-current`.
func EndByID(id, verdict string) (*Incident, error) {
	inc, err := LoadByID(id)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	inc.Store.Meta.Ended = now
	if verdict != "" {
		inc.Store.Meta.Verdict = verdict
	}
	if err := inc.Store.Save(); err != nil {
		return nil, fmt.Errorf("saving end marker: %w", err)
	}
	inc.Ended = now
	inc.Verdict = inc.Store.Meta.Verdict
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
