// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package build

import (
	"testing"

	"github.com/mgt-tool/mgtt/internal/model"
)

func TestComputeDiff_NoChanges(t *testing.T) {
	a := &model.Model{Components: map[string]*model.Component{
		"api": {Name: "api", Type: "deployment"},
	}}
	b := &model.Model{Components: map[string]*model.Component{
		"api": {Name: "api", Type: "deployment"},
	}}
	d := ComputeDiff(a, b)
	if len(d.Added) != 0 || len(d.Removed) != 0 || len(d.TypeChanged) != 0 {
		t.Errorf("unexpected diff: %+v", d)
	}
}

func TestComputeDiff_Added(t *testing.T) {
	prev := &model.Model{Components: map[string]*model.Component{
		"api": {Name: "api", Type: "deployment"},
	}}
	next := &model.Model{Components: map[string]*model.Component{
		"api": {Name: "api", Type: "deployment"},
		"rds": {Name: "rds", Type: "rds_instance"},
	}}
	d := ComputeDiff(prev, next)
	if len(d.Added) != 1 || d.Added[0] != "rds" {
		t.Errorf("want Added=[rds]; got %v", d.Added)
	}
	if len(d.Removed) != 0 {
		t.Errorf("want no removals; got %v", d.Removed)
	}
}

func TestComputeDiff_Removed(t *testing.T) {
	prev := &model.Model{Components: map[string]*model.Component{
		"api": {Name: "api", Type: "deployment"},
		"rds": {Name: "rds", Type: "rds_instance"},
	}}
	next := &model.Model{Components: map[string]*model.Component{
		"api": {Name: "api", Type: "deployment"},
	}}
	d := ComputeDiff(prev, next)
	if len(d.Removed) != 1 || d.Removed[0] != "rds" {
		t.Errorf("want Removed=[rds]; got %v", d.Removed)
	}
}

func TestComputeDiff_TypeChanged(t *testing.T) {
	prev := &model.Model{Components: map[string]*model.Component{
		"api": {Name: "api", Type: "deployment"},
	}}
	next := &model.Model{Components: map[string]*model.Component{
		"api": {Name: "api", Type: "statefulset"},
	}}
	d := ComputeDiff(prev, next)
	if len(d.TypeChanged) != 1 || d.TypeChanged[0].Name != "api" {
		t.Errorf("want TypeChanged=[api]; got %v", d.TypeChanged)
	}
	if d.TypeChanged[0].From != "deployment" || d.TypeChanged[0].To != "statefulset" {
		t.Errorf("want deployment→statefulset; got %s→%s", d.TypeChanged[0].From, d.TypeChanged[0].To)
	}
}

func TestComputeDiff_NilPrev(t *testing.T) {
	next := &model.Model{Components: map[string]*model.Component{
		"api": {Name: "api", Type: "deployment"},
	}}
	d := ComputeDiff(nil, next)
	if len(d.Added) != 1 || len(d.Removed) != 0 {
		t.Errorf("want all additions; got %+v", d)
	}
}
