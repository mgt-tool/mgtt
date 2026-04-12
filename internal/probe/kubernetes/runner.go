// Package kubernetes provides a probe.Runner that extracts facts from
// Kubernetes resources by running kubectl and parsing the full JSON output
// in Go. This replaces fragile jsonpath/shell-based probes with proper
// type handling (e.g. 0 for missing readyReplicas, max across pods for
// restartCount).
package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"mgtt/internal/probe"
)

// Runner implements probe.Runner for Kubernetes resources.
type Runner struct{}

// New returns a new Kubernetes Runner.
func New() *Runner { return &Runner{} }

// CanProbe reports whether this runner supports the given component type and
// fact combination.
func (r *Runner) CanProbe(componentType, fact string) bool {
	switch componentType {
	case "deployment":
		switch fact {
		case "ready_replicas", "desired_replicas", "restart_count", "endpoints":
			return true
		}
	case "ingress":
		switch fact {
		case "upstream_count":
			return true
		}
	}
	return false
}

// Probe extracts a single fact for the named component by running kubectl
// and parsing the JSON response.
func (r *Runner) Probe(ctx context.Context, component, fact string, vars map[string]string) (probe.Result, error) {
	namespace := vars["namespace"]
	if namespace == "" {
		namespace = "default"
	}
	componentType := vars["type"]

	switch componentType {
	case "deployment":
		return r.probeDeployment(ctx, namespace, component, fact)
	case "ingress":
		return r.probeIngress(ctx, namespace, component, fact)
	}
	return probe.Result{}, fmt.Errorf("kubernetes runner: unknown type %q", componentType)
}

func (r *Runner) probeDeployment(ctx context.Context, namespace, name, fact string) (probe.Result, error) {
	switch fact {
	case "ready_replicas":
		data, err := kubectlJSON(ctx, "get", "deploy", name, "-n", namespace)
		if err != nil {
			return probe.Result{}, err
		}
		val := jsonInt(data, "status", "readyReplicas")
		return intResult(val), nil

	case "desired_replicas":
		data, err := kubectlJSON(ctx, "get", "deploy", name, "-n", namespace)
		if err != nil {
			return probe.Result{}, err
		}
		val := jsonInt(data, "spec", "replicas")
		return intResult(val), nil

	case "restart_count":
		data, err := kubectlJSON(ctx, "get", "pods", "-l", "app="+name, "-n", namespace)
		if err != nil {
			return probe.Result{}, err
		}
		val := maxRestartCount(data)
		return intResult(val), nil

	case "endpoints":
		data, err := kubectlJSON(ctx, "get", "endpoints", name, "-n", namespace)
		if err != nil {
			return probe.Result{}, err
		}
		val := countEndpointAddresses(data)
		return intResult(val), nil
	}
	return probe.Result{}, fmt.Errorf("unknown deployment fact: %s", fact)
}

func (r *Runner) probeIngress(ctx context.Context, namespace, name, fact string) (probe.Result, error) {
	if fact == "upstream_count" {
		data, err := kubectlJSON(ctx, "get", "endpoints", name, "-n", namespace)
		if err != nil {
			return probe.Result{}, err
		}
		val := countEndpointAddresses(data)
		return intResult(val), nil
	}
	return probe.Result{}, fmt.Errorf("unknown ingress fact: %s", fact)
}

// intResult builds a probe.Result for an integer value.
func intResult(val int) probe.Result {
	return probe.Result{Raw: fmt.Sprintf("%d", val), Parsed: val}
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
