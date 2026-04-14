// Package testutil provides reusable test helpers for code that exercises
// the mgtt CLI — intended for both mgtt's own tests and provider-author
// integration tests that want to drive mgtt in-process.
package testutil

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/cli"
)

// RunCLI executes an mgtt command in-process and returns combined stdout
// and stderr as a string. Fails the test if the command returns an error.
func RunCLI(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	cmd := cli.RootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("mgtt %v: %v\noutput:\n%s", args, err, buf.String())
	}
	return buf.String()
}

// Golden compares `actual` against the contents of the file at `path`. When
// MGTT_UPDATE_GOLDEN is set in the environment, the file is overwritten
// instead of compared — the standard "golden file" workflow.
func Golden(t *testing.T, path, actual string) {
	t.Helper()
	if os.Getenv("MGTT_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with MGTT_UPDATE_GOLDEN=1 to create)", path, err)
	}
	if strings.TrimSpace(string(expected)) != strings.TrimSpace(actual) {
		t.Fatalf("golden mismatch: %s\n--- expected ---\n%s\n--- actual ---\n%s",
			path, string(expected), actual)
	}
}
