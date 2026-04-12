package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// ProbeResult is the JSON structure written to stdout on success.
type ProbeResult struct {
	Value any    `json:"value"`
	Raw   string `json:"raw"`
}

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "usage: mgtt-provider-kubernetes probe <component> <fact> [--namespace NS] [--type TYPE]\n")
		os.Exit(1)
	}

	if os.Args[1] != "probe" {
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}

	component := os.Args[2]
	fact := os.Args[3]

	// Parse flags.
	namespace := "default"
	componentType := "deployment"
	for i := 4; i < len(os.Args)-1; i++ {
		switch os.Args[i] {
		case "--namespace":
			namespace = os.Args[i+1]
		case "--type":
			componentType = os.Args[i+1]
		}
	}

	result, err := probe(context.Background(), namespace, componentType, component, fact)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe error: %v\n", err)
		os.Exit(1)
	}

	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "encode result: %v\n", err)
		os.Exit(1)
	}
}

// probe dispatches to the appropriate probe function based on componentType.
func probe(ctx context.Context, namespace, componentType, component, fact string) (ProbeResult, error) {
	switch componentType {
	case "deployment":
		return probeDeployment(ctx, namespace, component, fact)
	case "ingress":
		return probeIngress(ctx, namespace, component, fact)
	}
	return ProbeResult{}, fmt.Errorf("kubernetes runner: unknown type %q", componentType)
}

func probeDeployment(ctx context.Context, namespace, name, fact string) (ProbeResult, error) {
	switch fact {
	case "ready_replicas":
		data, err := kubectlJSON(ctx, "get", "deploy", name, "-n", namespace)
		if err != nil {
			return ProbeResult{}, err
		}
		val := jsonInt(data, "status", "readyReplicas")
		return intResult(val), nil

	case "desired_replicas":
		data, err := kubectlJSON(ctx, "get", "deploy", name, "-n", namespace)
		if err != nil {
			return ProbeResult{}, err
		}
		val := jsonInt(data, "spec", "replicas")
		return intResult(val), nil

	case "restart_count":
		data, err := kubectlJSON(ctx, "get", "pods", "-l", "app="+name, "-n", namespace)
		if err != nil {
			return ProbeResult{}, err
		}
		val := maxRestartCount(data)
		return intResult(val), nil

	case "endpoints":
		data, err := kubectlJSON(ctx, "get", "endpoints", name, "-n", namespace)
		if err != nil {
			return ProbeResult{}, err
		}
		val := countEndpointAddresses(data)
		return intResult(val), nil
	}
	return ProbeResult{}, fmt.Errorf("unknown deployment fact: %s", fact)
}

func probeIngress(ctx context.Context, namespace, name, fact string) (ProbeResult, error) {
	if fact == "upstream_count" {
		data, err := kubectlJSON(ctx, "get", "endpoints", name, "-n", namespace)
		if err != nil {
			return ProbeResult{}, err
		}
		val := countEndpointAddresses(data)
		return intResult(val), nil
	}
	return ProbeResult{}, fmt.Errorf("unknown ingress fact: %s", fact)
}

// intResult builds a ProbeResult for an integer value.
func intResult(val int) ProbeResult {
	return ProbeResult{Value: val, Raw: fmt.Sprintf("%d", val)}
}

// kubectlJSON runs kubectl with -o json and returns the parsed response.
func kubectlJSON(ctx context.Context, args ...string) (map[string]any, error) {
	fullArgs := append(args, "-o", "json")
	cmd := exec.CommandContext(ctx, "kubectl", fullArgs...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl %v: %w", args, err)
	}
	var data map[string]any
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, fmt.Errorf("parse kubectl output: %w", err)
	}
	return data, nil
}

// jsonInt traverses a nested map by the given key path and returns the value
// as an int. Returns 0 for any missing or non-numeric field.
func jsonInt(data map[string]any, path ...string) int {
	current := any(data)
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return 0
		}
		current = m[key]
	}
	switch v := current.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case nil:
		return 0
	}
	return 0
}

// maxRestartCount finds the maximum restartCount across all containers in all
// pods in a pod list response.
func maxRestartCount(data map[string]any) int {
	items, _ := data["items"].([]any)
	maxVal := 0
	for _, item := range items {
		pod, _ := item.(map[string]any)
		status, _ := pod["status"].(map[string]any)
		containers, _ := status["containerStatuses"].([]any)
		for _, c := range containers {
			cs, _ := c.(map[string]any)
			if v, ok := cs["restartCount"].(float64); ok && int(v) > maxVal {
				maxVal = int(v)
			}
		}
	}
	return maxVal
}

// countEndpointAddresses counts the total number of addresses across all
// subsets in an Endpoints resource. Returns 0 if there are no subsets.
func countEndpointAddresses(data map[string]any) int {
	subsets, _ := data["subsets"].([]any)
	count := 0
	for _, s := range subsets {
		subset, _ := s.(map[string]any)
		addrs, _ := subset["addresses"].([]any)
		count += len(addrs)
	}
	return count
}
