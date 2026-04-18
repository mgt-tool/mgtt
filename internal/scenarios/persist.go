package scenarios

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

const generatedHeader = `# GENERATED — rebuild via ` + "`mgtt validate --write-scenarios`" + `. Do not hand-edit.`

type diskFile struct {
	SourceHash string         `yaml:"source_hash"`
	Scenarios  []diskScenario `yaml:"scenarios"`
}

type diskScenario struct {
	ID    string     `yaml:"id"`
	Root  diskRoot   `yaml:"root"`
	Chain []diskStep `yaml:"chain"`
}

type diskRoot struct {
	Component string `yaml:"component"`
	State     string `yaml:"state"`
}

type diskStep struct {
	Component   string   `yaml:"component"`
	State       string   `yaml:"state"`
	EmitsOnEdge string   `yaml:"emits_on_edge,omitempty"`
	Observes    []string `yaml:"observes,omitempty"`
}

func Write(w io.Writer, sourceHash string, scs []Scenario) error {
	if _, err := fmt.Fprintln(w, generatedHeader); err != nil {
		return err
	}
	out := diskFile{SourceHash: sourceHash, Scenarios: make([]diskScenario, 0, len(scs))}
	for _, s := range scs {
		ds := diskScenario{
			ID:   s.ID,
			Root: diskRoot{Component: s.Root.Component, State: s.Root.State},
		}
		for _, step := range s.Chain {
			ds.Chain = append(ds.Chain, diskStep{
				Component:   step.Component,
				State:       step.State,
				EmitsOnEdge: step.EmitsOnEdge,
				Observes:    step.Observes,
			})
		}
		out.Scenarios = append(out.Scenarios, ds)
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(&out)
}

func Read(r io.Reader) ([]Scenario, string, error) {
	var in diskFile
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(&in); err != nil {
		return nil, "", fmt.Errorf("scenarios.yaml: %w", err)
	}
	out := make([]Scenario, 0, len(in.Scenarios))
	for _, ds := range in.Scenarios {
		s := Scenario{
			ID:   ds.ID,
			Root: RootRef{Component: ds.Root.Component, State: ds.Root.State},
		}
		for _, step := range ds.Chain {
			s.Chain = append(s.Chain, Step{
				Component:   step.Component,
				State:       step.State,
				EmitsOnEdge: step.EmitsOnEdge,
				Observes:    step.Observes,
			})
		}
		out = append(out, s)
	}
	return out, in.SourceHash, nil
}

// IndexEntry is one row in scenarios.index.yaml describing the per-model
// scenarios file that belongs to a workspace model.
type IndexEntry struct {
	Name          string `yaml:"name"`
	ModelPath     string `yaml:"path"`
	ScenariosPath string `yaml:"scenarios"`
	Hash          string `yaml:"hash"`
	Count         int    `yaml:"count"`
}

// ComputeSourceHash returns a stable sha256: prefix hash over the model
// file contents plus every type YAML referenced. Type paths are sorted
// to avoid order sensitivity.
func ComputeSourceHash(modelPath string, typePaths []string) (string, error) {
	h := sha256.New()
	readAndHash := func(p string) error {
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		h.Write(data)
		h.Write([]byte{0})
		return nil
	}
	if err := readAndHash(modelPath); err != nil {
		return "", err
	}
	sorted := make([]string, len(typePaths))
	copy(sorted, typePaths)
	sort.Strings(sorted)
	for _, p := range sorted {
		if err := readAndHash(p); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

type indexFile struct {
	Models []IndexEntry `yaml:"models"`
}

func WriteIndex(w io.Writer, entries []IndexEntry) error {
	if _, err := fmt.Fprintln(w, generatedHeader); err != nil {
		return err
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(indexFile{Models: entries})
}

func ReadIndex(r io.Reader) ([]IndexEntry, error) {
	var in indexFile
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("scenarios.index.yaml: %w", err)
	}
	return in.Models, nil
}
