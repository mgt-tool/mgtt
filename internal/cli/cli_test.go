package cli_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mgt-tool/mgtt/testutil"
)

func goldenPath(name string) string {
	_, file, _, _ := runtime.Caller(1)
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..")
	return filepath.Join(repoRoot, "testdata", "golden", name)
}

// TestGolden_StdlibLs verifies `mgtt stdlib ls` output. Stdlib is built in —
// the test has no provider or model dependencies.
func TestGolden_StdlibLs(t *testing.T) {
	actual := testutil.RunCLI(t, "stdlib", "ls")
	testutil.Golden(t, goldenPath("stdlib_ls.txt"), actual)
}
