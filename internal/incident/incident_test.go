package incident_test

import (
	"os"
	"testing"

	"github.com/sajonaro/mgtt/internal/incident"
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
