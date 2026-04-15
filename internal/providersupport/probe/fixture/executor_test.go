package fixture

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mgt-tool/mgtt/internal/providersupport/probe"
)

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "fixture.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFixtureExecutor_DefaultsStatusOkOnSuccess(t *testing.T) {
	p := writeFixture(t, `
kubernetes:
  api:
    ready_replicas:
      stdout: "3\n"
      exit: 0
`)
	ex, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ex.Run(context.Background(), probe.Command{
		Provider: "kubernetes", Component: "api", Fact: "ready_replicas", Parse: "int",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != probe.StatusOk {
		t.Fatalf("want StatusOk by default, got %q", res.Status)
	}
}

func TestFixtureExecutor_NotFoundStatus(t *testing.T) {
	p := writeFixture(t, `
kubernetes:
  api:
    ready_replicas:
      status: not_found
`)
	ex, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ex.Run(context.Background(), probe.Command{
		Provider: "kubernetes", Component: "api", Fact: "ready_replicas", Parse: "int",
	})
	if err != nil {
		t.Fatalf("not_found should not be an error: %v", err)
	}
	if res.Status != probe.StatusNotFound {
		t.Fatalf("want StatusNotFound, got %q", res.Status)
	}
	if res.Parsed != nil {
		t.Fatalf("not_found should leave Parsed nil, got %v", res.Parsed)
	}
}
