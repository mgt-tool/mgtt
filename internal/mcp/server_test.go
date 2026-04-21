package mcp

import (
	"encoding/json"
	"testing"
)

func TestAbout_ReturnsServerMetadata(t *testing.T) {
	h := NewHandler(Config{})
	result, err := h.About()
	if err != nil {
		t.Fatalf("About() returned error: %v", err)
	}
	if result.Version == "" {
		t.Error("expected non-empty version")
	}
	if len(result.Transports) == 0 {
		t.Error("expected at least one transport reported")
	}
	if _, err := json.Marshal(result); err != nil {
		t.Errorf("About result not JSON-serialisable: %v", err)
	}
}
