// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package simulate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mgt-tool/mgtt/internal/expr"
	"github.com/mgt-tool/mgtt/internal/facts"
	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// healthyTwoCompModel is twoCompModel with `status == up` as each type's
// healthy rule, so a component seen "down" is not cleared.
func healthyTwoCompModel(t *testing.T) (*model.Model, *providersupport.Registry) {
	t.Helper()
	m, reg := twoCompModel(t)
	p, _ := reg.Get("p")
	for _, ty := range p.Types {
		ty.Healthy = []expr.Node{&expr.CmpNode{Fact: "status", Op: expr.OpEq, Value: "up"}}
	}
	return m, reg
}

// recordInto runs RecordIncident with the model file anchored in dir.
func recordInto(t *testing.T, dir string, inc *incident.Incident) (*Recording, error) {
	t.Helper()
	m, reg := healthyTwoCompModel(t)
	return RecordIncident(inc, func() (*model.Model, *providersupport.Registry, string, error) {
		return m, reg, filepath.Join(dir, "system.model.yaml"), nil
	})
}

func value(key string, v any) facts.Fact { return facts.Fact{Key: key, Value: v} }
func status(key string, s facts.FactStatus) facts.Fact {
	return facts.Fact{Key: key, Status: s}
}

func TestRecordIncident_WritesScenarioThatPasses(t *testing.T) {
	cases := []struct {
		name    string
		verdict string
		facts   map[string][]facts.Fact
		want    string
	}{
		{
			name:    "both down names the upstream root",
			verdict: "db disk full",
			facts: map[string][]facts.Fact{
				"db":  {value("status", "down")},
				"web": {value("status", "down")},
			},
			want: `name: inc-rec
description: 'Recorded from incident inc-rec on 2026-10-04. Verdict: db disk full'
inject:
  db:
    status: down
  web:
    status: down
expect:
  root_cause: db
`,
		},
		{
			name: "all healthy is none with both eliminated",
			facts: map[string][]facts.Fact{
				"db":  {value("status", "up")},
				"web": {value("status", "up")},
			},
			want: `name: inc-rec
description: Recorded from incident inc-rec on 2026-10-04.
inject:
  db:
    status: up
  web:
    status: up
expect:
  root_cause: none
  eliminated: [db, web]
`,
		},
		{
			name: "last observation wins",
			facts: map[string][]facts.Fact{
				"db":  {value("status", "down"), value("status", "up")},
				"web": {value("status", "up")},
			},
			want: `name: inc-rec
description: Recorded from incident inc-rec on 2026-10-04.
inject:
  db:
    status: up
  web:
    status: up
expect:
  root_cause: none
  eliminated: [db, web]
`,
		},
		{
			name: "forbidden goes to unresolved and cannot be ruled out",
			facts: map[string][]facts.Fact{
				"db":  {status("status", facts.FactStatusForbidden)},
				"web": {value("status", "down")},
			},
			want: `name: inc-rec
description: Recorded from incident inc-rec on 2026-10-04.
inject:
  web:
    status: down
unresolved:
  db:
    status: forbidden
expect:
  root_cause: web
  cannot_rule_out: [db]
`,
		},
		{
			name: "not_found goes to unresolved",
			facts: map[string][]facts.Fact{
				"db":  {status("status", facts.FactStatusNotFound)},
				"web": {value("status", "down")},
			},
			want: `name: inc-rec
description: Recorded from incident inc-rec on 2026-10-04.
inject:
  web:
    status: down
unresolved:
  db:
    status: not_found
expect:
  root_cause: db
`,
		},
		{
			name: "a fact with no value and no status is listed, not recorded",
			facts: map[string][]facts.Fact{
				"db":  {value("status", "up"), {Key: "note"}},
				"web": {value("status", "up")},
			},
			want: `# Not recorded (no value and no probe outcome): db.note
name: inc-rec
description: Recorded from incident inc-rec on 2026-10-04.
inject:
  db:
    status: up
  web:
    status: up
expect:
  root_cause: none
  eliminated: [db, web]
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store := facts.NewInMemory()
			for comp, fs := range tc.facts {
				for _, f := range fs {
					store.Append(comp, f)
				}
			}
			inc := &incident.Incident{
				ID:      "inc-rec",
				Started: time.Date(2026, 10, 4, 9, 30, 0, 0, time.UTC),
				Verdict: tc.verdict,
				Store:   store,
			}
			rec, err := recordInto(t, dir, inc)
			if err != nil {
				t.Fatalf("RecordIncident: %v", err)
			}
			if want := filepath.Join(dir, "scenarios", "inc-rec.yaml"); rec.Path != want {
				t.Errorf("path = %s, want %s", rec.Path, want)
			}
			if string(rec.YAML) != tc.want {
				t.Errorf("yaml:\n%s\nwant:\n%s", rec.YAML, tc.want)
			}
			onDisk, err := os.ReadFile(rec.Path)
			if err != nil || string(onDisk) != string(rec.YAML) {
				t.Errorf("file on disk differs from returned YAML (err %v)", err)
			}
			if !rec.Result.Pass {
				t.Errorf("written scenario fails on replay: expected %+v, actual %+v", rec.Result.Scenario.Expect, rec.Result.Actual)
			}
		})
	}
}

func TestRecordIncident_NoFacts(t *testing.T) {
	inc := &incident.Incident{ID: "inc-empty", Store: facts.NewInMemory()}
	loaded := false
	_, err := RecordIncident(inc, func() (*model.Model, *providersupport.Registry, string, error) {
		loaded = true
		return nil, nil, "", errors.New("must not load")
	})
	if !errors.Is(err, ErrNoFacts) {
		t.Fatalf("err = %v, want ErrNoFacts", err)
	}
	if loaded {
		t.Error("model loaded for an incident with no facts")
	}
}

func TestRecordIncident_RefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenarios", "inc-rec.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("hand edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := facts.NewInMemory()
	store.Append("db", value("status", "down"))
	_, err := recordInto(t, dir, &incident.Incident{ID: "inc-rec", Store: store})
	var exists *ScenarioExistsError
	if !errors.As(err, &exists) || exists.Path != path {
		t.Fatalf("err = %v, want ScenarioExistsError for %s", err, path)
	}
	if data, _ := os.ReadFile(path); string(data) != "hand edited\n" {
		t.Errorf("existing file was modified: %q", data)
	}
}
