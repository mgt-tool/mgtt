package scenarios

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestWriteScenariosYAML_RoundTrip(t *testing.T) {
	scs := []Scenario{
		{
			ID:   "s-0001",
			Root: RootRef{Component: "rds", State: "stopped"},
			Chain: []Step{
				{Component: "rds", State: "stopped", EmitsOnEdge: "query_timeout"},
				{Component: "api", State: "crashed", EmitsOnEdge: "5xx"},
				{Component: "nginx", State: "degraded", Observes: []string{"upstream_count"}},
			},
		},
	}
	hash := "sha256:abc"
	var buf bytes.Buffer
	if err := Write(&buf, hash, scs); err != nil {
		t.Fatal(err)
	}
	got, gotHash, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if gotHash != hash {
		t.Errorf("hash roundtrip: want %q got %q", hash, gotHash)
	}
	if len(got) != 1 || got[0].ID != "s-0001" {
		t.Errorf("unexpected: %+v", got)
	}
	if got[0].Chain[0].EmitsOnEdge != "query_timeout" {
		t.Error("non-terminal step lost EmitsOnEdge")
	}
	if len(got[0].Chain[2].Observes) != 1 || got[0].Chain[2].Observes[0] != "upstream_count" {
		t.Error("terminal step lost Observes")
	}
}

func TestComputeSourceHash_Deterministic(t *testing.T) {
	modelPath := t.TempDir() + "/model.yaml"
	if err := os.WriteFile(modelPath, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	typePaths := []string{modelPath}
	h1, err := ComputeSourceHash(modelPath, typePaths)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := ComputeSourceHash(modelPath, typePaths)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Error("hash not deterministic")
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("want sha256: prefix; got %q", h1)
	}
}

func TestWriteIndexYAML_RoundTrip(t *testing.T) {
	entries := []IndexEntry{
		{Name: "prod", ModelPath: "environments/prod/model.yaml", ScenariosPath: "environments/prod/scenarios.yaml", Hash: "sha256:abc", Count: 42},
		{Name: "staging", ModelPath: "environments/staging/model.yaml", ScenariosPath: "environments/staging/scenarios.yaml", Hash: "sha256:def", Count: 38},
	}
	var buf bytes.Buffer
	if err := WriteIndex(&buf, entries); err != nil {
		t.Fatal(err)
	}
	got, err := ReadIndex(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "prod" || got[1].Count != 38 {
		t.Errorf("unexpected: %+v", got)
	}
}
