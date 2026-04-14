// Package fixture provides a probe.Executor backed by a YAML fixture file.
// It is used during testing and the troubleshooting demo to replay pre-recorded
// probe outputs without running real CLI commands.
package fixture

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
)

// fixtureEntry holds the raw stdout and exit code for a single probe.
type fixtureEntry struct {
	Stdout string `yaml:"stdout"`
	Exit   int    `yaml:"exit"`
}

// Executor is a probe.Executor that returns values from a loaded fixture file.
type Executor struct {
	// data maps provider → component → fact → entry.
	data map[string]map[string]map[string]fixtureEntry
}

// Load reads a fixture YAML file from path and returns an Executor.
// The YAML structure is:
//
//	<provider>:
//	  <component>:
//	    <fact>:
//	      stdout: "...\n"
//	      exit: 0
func Load(path string) (*Executor, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fixture: read %q: %w", path, err)
	}

	var data map[string]map[string]map[string]fixtureEntry
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("fixture: parse %q: %w", path, err)
	}

	return &Executor{data: data}, nil
}

// Run looks up the fixture entry for cmd.Provider/cmd.Component/cmd.Fact,
// parses the stdout using cmd.Parse, and returns the result.
func (e *Executor) Run(_ context.Context, cmd probe.Command) (probe.Result, error) {
	provMap, ok := e.data[cmd.Provider]
	if !ok {
		return probe.Result{}, fmt.Errorf("fixture not found: provider %q", cmd.Provider)
	}
	compMap, ok := provMap[cmd.Component]
	if !ok {
		return probe.Result{}, fmt.Errorf("fixture not found: %s.%s", cmd.Provider, cmd.Component)
	}
	entry, ok := compMap[cmd.Fact]
	if !ok {
		return probe.Result{}, fmt.Errorf("fixture not found: %s.%s.%s", cmd.Provider, cmd.Component, cmd.Fact)
	}

	parsed, err := probe.ParseOutput(cmd.Parse, entry.Stdout, entry.Exit)
	if err != nil {
		return probe.Result{Raw: entry.Stdout}, err
	}
	return probe.Result{Raw: entry.Stdout, Parsed: parsed}, nil
}
