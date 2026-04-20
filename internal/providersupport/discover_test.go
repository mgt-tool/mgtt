package providersupport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/sdk/provider"
)

// stubProviderBinary writes a tiny shell script that emits the given
// JSON on stdout (simulating a provider's `discover` subcommand) and
// returns its path.
func stubProviderBinary(t *testing.T, jsonOutput string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stub")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"discover\" ]; then\n" +
		"  cat <<'EOF'\n" + jsonOutput + "\nEOF\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo unknown >&2; exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInvokeDiscover_Happy(t *testing.T) {
	bin := stubProviderBinary(t, `{"components":[{"name":"api","type":"deployment"}],"dependencies":[{"from":"api","to":"rds"}]}`)
	res, err := InvokeDiscover(context.Background(), bin)
	if err != nil {
		t.Fatalf("InvokeDiscover: %v", err)
	}
	if len(res.Components) != 1 || res.Components[0].Name != "api" {
		t.Errorf("components: %+v", res.Components)
	}
	if len(res.Dependencies) != 1 || res.Dependencies[0].From != "api" {
		t.Errorf("dependencies: %+v", res.Dependencies)
	}
}

func TestInvokeDiscover_NotSupported(t *testing.T) {
	bin := stubProviderBinary(t, "")
	// The stub exits 1 on any command other than "discover", but
	// actually with jsonOutput="" the script emits an empty document
	// on discover too, which would parse as an empty DiscoveryResult —
	// so we need a different stub. Change it to one that exits 1 on
	// discover.
	// Instead: manually write a stub that always exits 1.
	dir := t.TempDir()
	path := filepath.Join(dir, "stub")
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = bin // unused on this path
	_, err := InvokeDiscover(context.Background(), path)
	if err == nil {
		t.Fatal("expected error for provider that doesn't support discover")
	}
	if !strings.Contains(err.Error(), "discover") {
		t.Errorf("error should mention discover; got: %v", err)
	}
}

var _ = provider.DiscoveryResult{}
