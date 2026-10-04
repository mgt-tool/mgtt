// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package providersupport

import (
	"context"
	"errors"
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
	dir := t.TempDir()
	path := filepath.Join(dir, "stub")
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := InvokeDiscover(context.Background(), path)
	if err == nil {
		t.Fatal("expected error for provider that doesn't support discover")
	}
	if !strings.Contains(err.Error(), "discover") {
		t.Errorf("error should mention discover; got: %v", err)
	}
}

// Exec succeeds but stdout isn't valid JSON. InvokeDiscover must
// surface a parse error distinguishable from an exit-non-zero
// failure — operators diagnosing a broken provider need to know
// which category they're in.
func TestInvokeDiscover_InvalidJSON(t *testing.T) {
	bin := stubProviderBinary(t, "not valid json")
	_, err := InvokeDiscover(context.Background(), bin)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse JSON") {
		t.Errorf("error should mention parse JSON; got: %v", err)
	}
}

var _ = provider.DiscoveryResult{}

// The SDK's refusal ("no RegisterDiscover call", or an SDK without the
// subcommand) is ErrNoDiscover; a discovery that ran and failed is not.
func TestInvokeDiscover_ClassifiesNoDiscover(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   bool
	}{
		{"not registered", "discover: this provider does not implement discovery (no RegisterDiscover call)", true},
		{"old sdk", "unknown command: discover", true},
		{"backend failure", "discover: backend API timeout", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "stub")
			script := "#!/bin/sh\necho '" + tc.stderr + "' >&2\nexit 1\n"
			if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			_, err := InvokeDiscover(context.Background(), path)
			if got := errors.Is(err, ErrNoDiscover); got != tc.want {
				t.Errorf("errors.Is(ErrNoDiscover) = %v, want %v (err: %v)", got, tc.want, err)
			}
		})
	}
}
