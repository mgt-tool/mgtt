package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVisualize_CommandWritesFile — run the cobra command against a
// fixture model in a temp dir and assert the output file contains
// the expected mermaid markers.
func TestVisualize_CommandWritesFile(t *testing.T) {
	t.Cleanup(func() {
		visualizeFlags.modelPath = ""
		visualizeFlags.outputPath = ""
	})
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.yaml")
	body := []byte("" +
		"meta:\n" +
		"  name: e2e\n" +
		"  version: \"1.0\"\n" +
		"components:\n" +
		"  api:\n" +
		"    type: service\n" +
		"    depends:\n" +
		"      - on: db\n" +
		"  db:\n" +
		"    type: rds_instance\n")
	if err := os.WriteFile(modelPath, body, 0o644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "diagram.md")
	var buf bytes.Buffer
	cmd := RootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"visualize", "--model", modelPath, "--output", outPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command failed: %v\noutput:\n%s", err, buf.String())
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	content := string(got)
	for _, want := range []string{"```mermaid", "graph LR", "api --> db"} {
		if !strings.Contains(content, want) {
			t.Errorf("output missing %q:\n%s", want, content)
		}
	}
}

// TestVisualize_CommandDefaultOutputPath — without --output, writes
// model-graph.md next to the model.
func TestVisualize_CommandDefaultOutputPath(t *testing.T) {
	t.Cleanup(func() {
		visualizeFlags.modelPath = ""
		visualizeFlags.outputPath = ""
	})
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.yaml")
	body := []byte("meta:\n  name: d\n  version: \"1.0\"\ncomponents:\n  a:\n    type: service\n")
	if err := os.WriteFile(modelPath, body, 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cmd := RootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"visualize", "--model", modelPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command failed: %v\noutput:\n%s", err, buf.String())
	}
	defaultPath := filepath.Join(dir, "model-graph.md")
	if _, err := os.Stat(defaultPath); err != nil {
		t.Errorf("default output file missing: %v", err)
	}
}

// TestVisualize_CommandMissingModel — explicit --model pointing at a
// non-existent file produces a clear error and non-zero exit.
func TestVisualize_CommandMissingModel(t *testing.T) {
	t.Cleanup(func() {
		visualizeFlags.modelPath = ""
		visualizeFlags.outputPath = ""
	})
	var buf bytes.Buffer
	cmd := RootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"visualize", "--model", "/tmp/does-not-exist-visualize-test.yaml"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for missing model; got nil")
	}
	if !strings.Contains(err.Error(), "load model") {
		t.Errorf("error should mention load model; got %v", err)
	}
}
