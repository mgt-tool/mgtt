package main

import "testing"

// ---------------------------------------------------------------------------
// jsonInt
// ---------------------------------------------------------------------------

func TestJsonInt_Normal(t *testing.T) {
	data := map[string]any{
		"status": map[string]any{
			"readyReplicas": float64(3),
		},
	}
	if got := jsonInt(data, "status", "readyReplicas"); got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
}

func TestJsonInt_Missing(t *testing.T) {
	data := map[string]any{
		"status": map[string]any{},
	}
	if got := jsonInt(data, "status", "readyReplicas"); got != 0 {
		t.Fatalf("expected 0 for missing field, got %d", got)
	}
}

func TestJsonInt_NoStatusBlock(t *testing.T) {
	data := map[string]any{}
	if got := jsonInt(data, "status", "readyReplicas"); got != 0 {
		t.Fatalf("expected 0 for missing status block, got %d", got)
	}
}

func TestJsonInt_NilValue(t *testing.T) {
	data := map[string]any{
		"spec": map[string]any{
			"replicas": nil,
		},
	}
	if got := jsonInt(data, "spec", "replicas"); got != 0 {
		t.Fatalf("expected 0 for nil value, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// maxRestartCount
// ---------------------------------------------------------------------------

func TestMaxRestartCount(t *testing.T) {
	data := map[string]any{
		"items": []any{
			map[string]any{
				"status": map[string]any{
					"containerStatuses": []any{
						map[string]any{"restartCount": float64(5)},
					},
				},
			},
			map[string]any{
				"status": map[string]any{
					"containerStatuses": []any{
						map[string]any{"restartCount": float64(12)},
					},
				},
			},
		},
	}
	if got := maxRestartCount(data); got != 12 {
		t.Fatalf("expected max 12, got %d", got)
	}
}

func TestMaxRestartCount_MultipleContainers(t *testing.T) {
	data := map[string]any{
		"items": []any{
			map[string]any{
				"status": map[string]any{
					"containerStatuses": []any{
						map[string]any{"restartCount": float64(2)},
						map[string]any{"restartCount": float64(8)},
					},
				},
			},
		},
	}
	if got := maxRestartCount(data); got != 8 {
		t.Fatalf("expected max 8, got %d", got)
	}
}

func TestMaxRestartCount_Empty(t *testing.T) {
	data := map[string]any{
		"items": []any{},
	}
	if got := maxRestartCount(data); got != 0 {
		t.Fatalf("expected 0 for empty items, got %d", got)
	}
}

func TestMaxRestartCount_NoItems(t *testing.T) {
	data := map[string]any{}
	if got := maxRestartCount(data); got != 0 {
		t.Fatalf("expected 0 for missing items, got %d", got)
	}
}

func TestMaxRestartCount_NoContainerStatuses(t *testing.T) {
	data := map[string]any{
		"items": []any{
			map[string]any{
				"status": map[string]any{},
			},
		},
	}
	if got := maxRestartCount(data); got != 0 {
		t.Fatalf("expected 0 for missing containerStatuses, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// countEndpointAddresses
// ---------------------------------------------------------------------------

func TestCountEndpointAddresses(t *testing.T) {
	data := map[string]any{
		"subsets": []any{
			map[string]any{
				"addresses": []any{
					map[string]any{"ip": "10.0.0.1"},
					map[string]any{"ip": "10.0.0.2"},
				},
			},
		},
	}
	if got := countEndpointAddresses(data); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
}

func TestCountEndpointAddresses_MultipleSubsets(t *testing.T) {
	data := map[string]any{
		"subsets": []any{
			map[string]any{
				"addresses": []any{
					map[string]any{"ip": "10.0.0.1"},
				},
			},
			map[string]any{
				"addresses": []any{
					map[string]any{"ip": "10.0.0.2"},
					map[string]any{"ip": "10.0.0.3"},
				},
			},
		},
	}
	if got := countEndpointAddresses(data); got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
}

func TestCountEndpointAddresses_NoSubsets(t *testing.T) {
	data := map[string]any{}
	if got := countEndpointAddresses(data); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestCountEndpointAddresses_EmptySubsets(t *testing.T) {
	data := map[string]any{
		"subsets": []any{},
	}
	if got := countEndpointAddresses(data); got != 0 {
		t.Fatalf("expected 0 for empty subsets, got %d", got)
	}
}

func TestCountEndpointAddresses_SubsetWithNoAddresses(t *testing.T) {
	data := map[string]any{
		"subsets": []any{
			map[string]any{},
		},
	}
	if got := countEndpointAddresses(data); got != 0 {
		t.Fatalf("expected 0 for subset with no addresses, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// intResult
// ---------------------------------------------------------------------------

func TestIntResult(t *testing.T) {
	r := intResult(42)
	if r.Raw != "42" {
		t.Fatalf("expected raw \"42\", got %q", r.Raw)
	}
	if r.Value != 42 {
		t.Fatalf("expected value 42, got %v", r.Value)
	}
}

func TestIntResult_Zero(t *testing.T) {
	r := intResult(0)
	if r.Raw != "0" {
		t.Fatalf("expected raw \"0\", got %q", r.Raw)
	}
	if r.Value != 0 {
		t.Fatalf("expected value 0, got %v", r.Value)
	}
}
