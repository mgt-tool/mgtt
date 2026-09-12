// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/cli"
)

// `mgtt version` is read by scripts -- the release workflow captures it to
// check every channel answers with the tag -- so it has to arrive on stdout.
// cobra's Println writes to stderr, which is how it once went missing.
func TestVersion_PrintsToStdout(t *testing.T) {
	var out, errOut bytes.Buffer
	cmd := cli.RootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.HasPrefix(out.String(), "mgtt version ") {
		t.Fatalf("stdout = %q, want it to begin with \"mgtt version \"", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want nothing", errOut.String())
	}
}
