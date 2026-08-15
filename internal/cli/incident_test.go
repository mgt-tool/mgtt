// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// writeTwoComponentModel writes a minimal model.yaml (api depends on db)
// matching the synthetic registry supplied by diagnoseFixture. The fixture
// registers a provider "p" whose types both expose a single "status" fact
// and a non-default "down" state whose predicate is `status == "down"`.
// Synthesizing the matching scenarios.yaml is delegated to callers.
func writeTwoComponentModel(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, "model.yaml")
	body := "meta:\n" +
		"  name: " + name + "\n" +
		"  version: 0.1.0\n" +
		"  providers: [p]\n" +
		"components:\n" +
		"  api:\n" +
		"    type: api\n" +
		"    depends:\n" +
		"      - on: db\n" +
		"  db:\n" +
		"    type: db\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	return path
}

// writeScenariosYAML creates a scenarios.yaml next to modelPath with a
// single chain: [{db,down,emits:<emits>}, {api,down,observes:[status]}].
func writeScenariosYAML(t *testing.T, modelPath, emits string) {
	t.Helper()
	scPath := filepath.Join(filepath.Dir(modelPath), "scenarios.yaml")
	body := "source_hash: sha256:test\n" +
		"scenarios:\n" +
		"  - id: s-0001\n" +
		"    root:\n" +
		"      component: db\n" +
		"      state: down\n" +
		"    chain:\n" +
		"      - component: db\n" +
		"        state: down\n" +
		"        emits_on_edge: " + emits + "\n" +
		"      - component: api\n" +
		"        state: down\n" +
		"        observes: [status]\n"
	if err := os.WriteFile(scPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write scenarios.yaml: %v", err)
	}
}

// stubLoadModelAndRegistry swaps suggestionLoaderHook to return
// diagnoseFixture's synthetic model + registry pair for the duration of
// the test. The modelPath returned points at the on-disk model.yaml the
// test wrote, so scenarios.yaml lookups still land on real files.
func stubLoadModelAndRegistry(t *testing.T, modelPath string) {
	t.Helper()
	prev := suggestionLoaderHook
	m, reg := diagnoseFixture(t)
	suggestionLoaderHook = func(_ string) (*model.Model, *providersupport.Registry, string, error) {
		return m, reg, modelPath, nil
	}
	t.Cleanup(func() { suggestionLoaderHook = prev })
}

// chdirTempDir establishes a temp dir as the test's working directory.
// t.Chdir (Go 1.24+) auto-restores the prior cwd on test end.
func chdirTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

// seedIncidentStore starts an incident on disk and appends facts that
// should evaluate the named components' non-default state predicates to
// true. The caller supplies (component, value) pairs.
func seedIncidentStore(t *testing.T, modelName, id string, values map[string]any) *incident.Incident {
	t.Helper()
	inc, err := incident.Start(modelName, "0.1.0", id)
	if err != nil {
		t.Fatalf("incident.Start: %v", err)
	}
	for comp, v := range values {
		if err := inc.Store.AppendAndSave(comp, facts.Fact{
			Key:       "status",
			Value:     v,
			Collector: "test",
			At:        time.Now(),
		}); err != nil {
			t.Fatalf("append fact: %v", err)
		}
	}
	return inc
}

// runEndWithSuggest invokes emitScenarioSuggestions directly against the
// active incident and returns captured stdout. The CLI path layers a
// cobra command around this helper; exercising it directly sidesteps
// cobra's global state and keeps tests hermetic.
func runEndWithSuggest(t *testing.T) string {
	t.Helper()
	inc, err := incident.End()
	if err != nil {
		t.Fatalf("incident.End: %v", err)
	}
	var buf bytes.Buffer
	if err := emitScenarioSuggestions(&buf, inc); err != nil {
		t.Fatalf("emitScenarioSuggestions: %v", err)
	}
	return buf.String()
}

// TestIncidentEnd_SuggestScenarios_ExistingChainMatches — scenarios.yaml
// already contains db.down → api.down. Facts reproducing that chain
// should produce "matches existing scenario" and no patch file.
func TestIncidentEnd_SuggestScenarios_ExistingChainMatches(t *testing.T) {
	dir := chdirTempDir(t)
	modelPath := writeTwoComponentModel(t, dir, "storefront-match")
	writeScenariosYAML(t, modelPath, "timeout")
	stubLoadModelAndRegistry(t, modelPath)

	seedIncidentStore(t, "storefront-match", "inc-match-001", map[string]any{
		"db":  "down",
		"api": "down",
	})

	out := runEndWithSuggest(t)
	if !strings.Contains(out, "matches existing scenario") {
		t.Fatalf("expected 'matches existing scenario' in output; got %q", out)
	}
	if _, err := os.Stat(filepath.Join(".mgtt", "pending-scenarios", "inc-match-001.patch")); err == nil {
		t.Fatal("patch file should not have been created for a matching chain")
	}
}

// TestIncidentEnd_SuggestScenarios_NovelChainEmitsPatch — scenarios.yaml
// has no chain; facts imply db.down → api.down; patch file must be
// created with the expected components in order.
func TestIncidentEnd_SuggestScenarios_NovelChainEmitsPatch(t *testing.T) {
	dir := chdirTempDir(t)
	modelPath := writeTwoComponentModel(t, dir, "storefront-novel")
	// no scenarios.yaml — novel chain
	stubLoadModelAndRegistry(t, modelPath)

	seedIncidentStore(t, "storefront-novel", "inc-novel-001", map[string]any{
		"db":  "down",
		"api": "down",
	})

	out := runEndWithSuggest(t)
	if !strings.Contains(out, "wrote") {
		t.Fatalf("expected 'wrote' in output; got %q", out)
	}

	patchPath := filepath.Join(".mgtt", "pending-scenarios", "inc-novel-001.patch")
	data, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("patch file missing: %v", err)
	}
	body := string(data)

	// Ordering sanity: db (upstream) should appear before api (downstream).
	dbIdx := strings.Index(body, "component: db")
	apiIdx := strings.Index(body, "component: api")
	if dbIdx < 0 || apiIdx < 0 {
		t.Fatalf("patch missing db or api component:\n%s", body)
	}
	if dbIdx >= apiIdx {
		t.Fatalf("db (upstream) should precede api (downstream) in chain:\n%s", body)
	}
	if !strings.Contains(body, "root: { component: db, state: down }") {
		t.Fatalf("root not set to db.down:\n%s", body)
	}
}

// TestIncidentEnd_SuggestScenarios_TrivialChainNoOutput — facts for one
// component only. Chain has <2 steps, so we report and don't emit a patch.
func TestIncidentEnd_SuggestScenarios_TrivialChainNoOutput(t *testing.T) {
	dir := chdirTempDir(t)
	modelPath := writeTwoComponentModel(t, dir, "storefront-trivial")
	stubLoadModelAndRegistry(t, modelPath)

	seedIncidentStore(t, "storefront-trivial", "inc-trivial-001", map[string]any{
		"db": "down",
	})

	out := runEndWithSuggest(t)
	if !strings.Contains(out, "no non-trivial chain") {
		t.Fatalf("expected 'no non-trivial chain' message; got %q", out)
	}
	if _, err := os.Stat(filepath.Join(".mgtt", "pending-scenarios", "inc-trivial-001.patch")); err == nil {
		t.Fatal("patch file should not be created for a single-step chain")
	}
}

// Ensure expr/facts imports are used even if test matrices shrink.
var _ = facts.Fact{}
var _ = expr.CmpNode{}
