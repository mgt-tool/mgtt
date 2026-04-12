package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mgtt/internal/cli"
)

var update = false

func init() {
	for _, arg := range os.Args {
		if arg == "-update" {
			update = true
		}
	}
}

// TestMain sets MGTT_HOME so LoadEmbedded can find providers from the repo
// providers/ directory (since embed.FS is not wired in tests).
func TestMain(m *testing.M) {
	_, file, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..")
	os.Setenv("MGTT_HOME", repoRoot)
	os.Exit(m.Run())
}

func runCommand(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	cmd := cli.RootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command %v failed: %v\noutput: %s", args, err, buf.String())
	}
	return buf.String()
}

func goldenTest(t *testing.T, path string, actual string) {
	t.Helper()
	if update {
		if err := os.WriteFile(path, []byte(actual), 0644); err != nil {
			t.Fatalf("write golden file %s: %v", path, err)
		}
		return
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v (run with -update to create)", path, err)
	}
	if strings.TrimSpace(string(expected)) != strings.TrimSpace(actual) {
		t.Fatalf("golden mismatch for %s.\n--- Expected ---\n%s\n--- Actual ---\n%s",
			path, string(expected), actual)
	}
}

// goldenPath returns the absolute path to a file under testdata/golden/,
// anchored to the repo root.
func goldenPath(name string) string {
	_, file, _, _ := runtime.Caller(1)
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..")
	return filepath.Join(repoRoot, "testdata", "golden", name)
}

// ---------------------------------------------------------------------------
// Golden tests
// ---------------------------------------------------------------------------

func TestGolden_ModelValidateStorefront(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..")
	modelPath := filepath.Join(repoRoot, "examples", "storefront", "system.model.yaml")
	actual := runCommand(t, "model", "validate", modelPath)
	goldenTest(t, goldenPath("model_validate_storefront.txt"), actual)
}

func TestGolden_StdlibLs(t *testing.T) {
	actual := runCommand(t, "stdlib", "ls")
	goldenTest(t, goldenPath("stdlib_ls.txt"), actual)
}

func TestGolden_SimulateAll(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..")
	modelPath := filepath.Join(repoRoot, "examples", "storefront", "system.model.yaml")
	scenariosPath := filepath.Join(repoRoot, "scenarios")
	actual := runCommand(t, "simulate", "--all", "--model", modelPath, "--scenarios-dir", scenariosPath)
	goldenTest(t, goldenPath("simulate_all.txt"), actual)
}
