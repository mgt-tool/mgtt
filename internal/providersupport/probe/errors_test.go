// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package probe

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyExit_MapsExitCodes(t *testing.T) {
	cases := []struct {
		code int
		want error
	}{
		{1, ErrUsage},
		{2, ErrEnv},
		{3, ErrForbidden},
		{4, ErrTransient},
		{5, ErrProtocol},
		{99, ErrUnknown},
	}
	for _, c := range cases {
		got := ClassifyExit(c.code, "stderr msg")
		if !errors.Is(got, c.want) {
			t.Errorf("exit %d: want %v, got %v", c.code, c.want, got)
		}
	}
}

func TestClassifyExit_IncludesStderrMessage(t *testing.T) {
	err := ClassifyExit(3, "forbidden: user x cannot get deployments")
	if err == nil || !strings.Contains(err.Error(), "forbidden: user x") {
		t.Fatalf("err should include stderr: %v", err)
	}
}

func TestClassifyExit_TrimsToFirstLine(t *testing.T) {
	err := ClassifyExit(2, "kubectl not found\nadditional debug noise\nmore noise")
	if strings.Contains(err.Error(), "additional") {
		t.Fatalf("only first line should appear: %v", err)
	}
}

func TestFirstLine(t *testing.T) {
	cases := map[string]string{
		"hello":        "hello",
		"hello\nworld": "hello",
		"  hello  ":    "hello",
		"":             "",
		"\nbody":       "",
		"line\n\nlast": "line",
	}
	for in, want := range cases {
		if got := FirstLine(in); got != want {
			t.Errorf("FirstLine(%q) = %q, want %q", in, got, want)
		}
	}
}
