package incident_test

import (
	"os"
	"testing"

	"github.com/mgt-tool/mgtt/internal/incident"
)

func TestStartAndEnd(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	inc, err := incident.Start("storefront", "1.0", "test-inc-001")
	if err != nil {
		t.Fatal(err)
	}
	if inc.ID != "test-inc-001" {
		t.Fatalf("expected ID 'test-inc-001', got %q", inc.ID)
	}

	// Verify .mgtt-current exists
	if _, err := os.Stat(".mgtt-current"); err != nil {
		t.Fatal("missing .mgtt-current")
	}

	// Verify Current() works
	cur, err := incident.Current()
	if err != nil {
		t.Fatal(err)
	}
	if cur.ID != "test-inc-001" {
		t.Fatalf("Current: expected ID 'test-inc-001', got %q", cur.ID)
	}

	// End
	ended, err := incident.End()
	if err != nil {
		t.Fatal(err)
	}
	if ended.ID != "test-inc-001" {
		t.Fatalf("End: wrong ID")
	}
	if ended.Ended.IsZero() {
		t.Fatal("Ended time not set")
	}

	// .mgtt-current should be gone
	if _, err := os.Stat(".mgtt-current"); !os.IsNotExist(err) {
		t.Fatal(".mgtt-current should be removed")
	}
}

func TestStart_AlreadyActive(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	_, err := incident.Start("test", "1.0", "inc-1")
	if err != nil {
		t.Fatal(err)
	}

	_, err = incident.Start("test", "1.0", "inc-2")
	if err == nil {
		t.Fatal("expected error for duplicate start")
	}
}

func TestEnd_NoActive(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	_, err := incident.End()
	if err == nil {
		t.Fatal("expected error when no active incident")
	}
}

func TestLoadByID_ReturnsIncidentFromDisk(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	_, err := incident.StartIsolated("storefront", "1.0", "inc-persistent-1", "")
	if err != nil {
		t.Fatalf("StartIsolated: %v", err)
	}
	loaded, err := incident.LoadByID("inc-persistent-1")
	if err != nil {
		t.Fatalf("LoadByID: %v", err)
	}
	if loaded.ID != "inc-persistent-1" {
		t.Errorf("ID: got %q want %q", loaded.ID, "inc-persistent-1")
	}
	if loaded.Model != "storefront" {
		t.Errorf("Model: got %q want %q", loaded.Model, "storefront")
	}
}

func TestLoadByID_MissingIncidentReturnsError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	if _, err := incident.LoadByID("does-not-exist"); err == nil {
		t.Fatal("expected error for missing incident id")
	}
}

func TestEndByID_MarksEndedAndVerdictInState(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	if _, err := incident.StartIsolated("storefront", "1.0", "inc-end-1", ""); err != nil {
		t.Fatalf("StartIsolated: %v", err)
	}
	ended, err := incident.EndByID("inc-end-1", "rds was the root cause")
	if err != nil {
		t.Fatalf("EndByID: %v", err)
	}
	if ended.Ended.IsZero() {
		t.Error("Ended time must be non-zero after EndByID")
	}

	// Persistence check — reload and confirm the ended marker + verdict are
	// on disk. A snapshot tool calling LoadByID later needs to see them.
	reloaded, err := incident.LoadByID("inc-end-1")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Ended.IsZero() {
		t.Error("reloaded Ended must be non-zero — verdict must survive round-trip")
	}
	if reloaded.Verdict != "rds was the root cause" {
		t.Errorf("reloaded Verdict: got %q want %q", reloaded.Verdict, "rds was the root cause")
	}
}

func TestEndByID_MissingIncidentReturnsError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	if _, err := incident.EndByID("nope", ""); err == nil {
		t.Fatal("expected error for missing incident")
	}
}

func TestEndByID_DoesNotTouchCLICurrentPointer(t *testing.T) {
	// D5 invariant: MCP end must not side-effect the CLI's single-active
	// pointer. A human CLI session running in parallel must survive.
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	// Simulate a concurrent CLI incident.
	if _, err := incident.Start("cli-model", "1.0", "inc-cli"); err != nil {
		t.Fatalf("CLI Start: %v", err)
	}
	// MCP-style isolated incident.
	if _, err := incident.StartIsolated("mcp-model", "1.0", "inc-mcp", ""); err != nil {
		t.Fatalf("StartIsolated: %v", err)
	}
	if _, err := incident.EndByID("inc-mcp", ""); err != nil {
		t.Fatalf("EndByID: %v", err)
	}
	// CLI's pointer must still point at inc-cli.
	if _, err := os.Stat(".mgtt-current"); err != nil {
		t.Fatal("MCP EndByID must not remove .mgtt-current")
	}
}

func TestGenerateID(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Start with empty ID — should auto-generate
	inc, err := incident.Start("test", "1.0", "")
	if err != nil {
		t.Fatal(err)
	}
	if inc.ID == "" {
		t.Fatal("expected generated ID, got empty string")
	}
}
