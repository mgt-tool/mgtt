// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// ErrNoFacts is returned by RecordIncident when the incident's fact store
// is empty: there is nothing to replay.
var ErrNoFacts = errors.New("incident recorded no facts — nothing to emit")

// ScenarioExistsError is returned by RecordIncident when the scenario file
// is already there. A recorded scenario may have been edited and committed
// since, so it is never overwritten.
type ScenarioExistsError struct{ Path string }

func (e *ScenarioExistsError) Error() string {
	return e.Path + " already exists — not overwritten"
}

// ModelLoader returns the model an incident was diagnosed against, its
// provider registry, and the model file's path. The CLI resolves the model
// by name, MCP by the incident's model_ref.
type ModelLoader func() (*model.Model, *providersupport.Registry, string, error)

// Recording is an incident written out as a simulate scenario.
type Recording struct {
	// Path is scenarios/<incident-id>.yaml next to the model file.
	Path string
	// YAML is the file's content.
	YAML []byte
	// Result is the written file loaded back and run, as `mgtt simulate
	// --scenario` would. It fails only if simulate and the evaluation that
	// produced the expectation disagree.
	Result *Result
}

// RecordIncident turns an ended incident into a hand-authored simulate
// scenario: the last observation of every fact goes into inject: (a value)
// or unresolved: (not_found, forbidden, transient), and expect: is what
// the engine concludes from exactly those facts. The file is written next
// to the model and then replayed once.
func RecordIncident(inc *incident.Incident, load ModelLoader) (*Recording, error) {
	inject, unresolved, skipped := lastObservations(inc.Store)
	if len(inject) == 0 && len(unresolved) == 0 && len(skipped) == 0 {
		return nil, ErrNoFacts
	}
	m, reg, modelPath, err := load()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Dir(modelPath), "scenarios", inc.ID+".yaml")
	if _, err := os.Stat(path); err == nil {
		return nil, &ScenarioExistsError{Path: path}
	}

	sc := &Scenario{
		Name:        inc.ID,
		Description: recordedDescription(inc),
		Inject:      inject,
		Unresolved:  unresolved,
	}
	actual := Run(m, reg, sc).Actual
	sc.Expect = Expectation{
		RootCause:          actual.RootCause,
		Eliminated:         actual.Eliminated,
		CannotRuleOut:      actual.CannotRuleOut,
		RedundancyDegraded: actual.RedundancyDegraded,
	}

	data, err := marshalRecorded(sc, skipped)
	if err != nil {
		return nil, err
	}
	if err := writeNew(path, data); err != nil {
		return nil, err
	}
	written, err := LoadScenario(path)
	if err != nil {
		return nil, err
	}
	return &Recording{Path: path, YAML: data, Result: Run(m, reg, written)}, nil
}

// lastObservations reduces the store to the latest record of each
// (component, fact), the same record the engine reads. A record with
// neither a value nor a probe status cannot be expressed in a scenario;
// it is returned as "component.fact" in skipped.
func lastObservations(store *facts.Store) (inject map[string]map[string]any, unresolved map[string]map[string]string, skipped []string) {
	inject = map[string]map[string]any{}
	unresolved = map[string]map[string]string{}
	if store == nil {
		return inject, unresolved, nil
	}
	for _, comp := range store.AllComponents() {
		latest := map[string]facts.Fact{}
		for _, f := range store.FactsFor(comp) {
			latest[f.Key] = f
		}
		for key, f := range latest {
			switch {
			case f.Value != nil:
				if inject[comp] == nil {
					inject[comp] = map[string]any{}
				}
				inject[comp][key] = f.Value
			case unresolvedStatuses[string(f.Status)] != "":
				if unresolved[comp] == nil {
					unresolved[comp] = map[string]string{}
				}
				unresolved[comp][key] = string(f.Status)
			default:
				skipped = append(skipped, comp+"."+key)
			}
		}
	}
	sort.Strings(skipped)
	return inject, unresolved, skipped
}

func recordedDescription(inc *incident.Incident) string {
	d := fmt.Sprintf("Recorded from incident %s on %s.", inc.ID, inc.Started.Format("2006-01-02"))
	if inc.Verdict != "" {
		d += " Verdict: " + inc.Verdict
	}
	return d
}

// recordedScenario is Scenario as written by RecordIncident: the same
// keys, with the ones the recording does not use left out.
type recordedScenario struct {
	Name        string                       `yaml:"name"`
	Description string                       `yaml:"description"`
	Inject      map[string]map[string]any    `yaml:"inject"`
	Unresolved  map[string]map[string]string `yaml:"unresolved,omitempty"`
	Expect      struct {
		RootCause          string   `yaml:"root_cause"`
		Eliminated         []string `yaml:"eliminated,omitempty,flow"`
		CannotRuleOut      []string `yaml:"cannot_rule_out,omitempty,flow"`
		RedundancyDegraded []string `yaml:"redundancy_degraded,omitempty,flow"`
	} `yaml:"expect"`
}

func marshalRecorded(sc *Scenario, skipped []string) ([]byte, error) {
	out := recordedScenario{
		Name:        sc.Name,
		Description: sc.Description,
		Inject:      sc.Inject,
		Unresolved:  sc.Unresolved,
	}
	out.Expect.RootCause = sc.Expect.RootCause
	out.Expect.Eliminated = sc.Expect.Eliminated
	out.Expect.CannotRuleOut = sc.Expect.CannotRuleOut
	out.Expect.RedundancyDegraded = sc.Expect.RedundancyDegraded

	var buf bytes.Buffer
	if len(skipped) > 0 {
		fmt.Fprintf(&buf, "# Not recorded (no value and no probe outcome): %s\n", strings.Join(skipped, ", "))
	}
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return nil, fmt.Errorf("encode scenario: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode scenario: %w", err)
	}
	return buf.Bytes(), nil
}

// writeNew creates path and its directory, refusing to replace a file
// that appeared since the caller's existence check.
func writeNew(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return &ScenarioExistsError{Path: path}
	}
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	return f.Close()
}
