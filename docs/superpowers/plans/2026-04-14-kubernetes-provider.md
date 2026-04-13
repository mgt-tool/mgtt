# Kubernetes Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the mgtt Kubernetes provider from 2 types to 37, covering every K8s resource with meaningful troubleshooting state, and refactor the provider loader to support multi-file type definitions.

**Architecture:** Three orthogonal changes: (1) add `age_seconds` parse mode to the probe parser, (2) extend `LoadFromFile` to support a directory containing `provider.yaml` + `types/*.yaml`, (3) create the 37 type YAML files under `providers/kubernetes/types/`. Changes are additive — existing single-file providers and all current tests continue to work unchanged.

**Tech Stack:** Go, YAML (`gopkg.in/yaml.v3`), existing mgtt expression compiler

---

## Task 1: Add `age_seconds` parse mode

Several K8s types need to compute "seconds since timestamp" from ISO 8601 values returned by kubectl. This is a new parse mode in the probe parser.

**Files:**
- Modify: `internal/providersupport/probe/parse.go:22-74`
- Modify: `internal/providersupport/probe/probe_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/providersupport/probe/probe_test.go`:

```go
// ---------------------------------------------------------------------------
// ParseOutput — age_seconds
// ---------------------------------------------------------------------------

func TestParseOutput_AgeSeconds(t *testing.T) {
	// Use a timestamp 120 seconds in the past.
	ts := time.Now().Add(-120 * time.Second).UTC().Format(time.RFC3339)
	v, err := probe.ParseOutput("age_seconds", ts+"\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	age, ok := v.(int)
	if !ok {
		t.Fatalf("expected int, got %T", v)
	}
	// Allow 5 seconds of clock skew.
	if age < 115 || age > 125 {
		t.Fatalf("expected age ~120, got %d", age)
	}
}

func TestParseOutput_AgeSeconds_Empty(t *testing.T) {
	// Empty timestamp (field not set) should return 0.
	v, err := probe.ParseOutput("age_seconds", "\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, v, 0)
}

func TestParseOutput_AgeSeconds_InvalidFormat(t *testing.T) {
	_, err := probe.ParseOutput("age_seconds", "not-a-timestamp\n", 0)
	if err == nil {
		t.Fatal("expected error for invalid timestamp")
	}
}
```

Add `"time"` to the import block in the test file.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/probe/ -run TestParseOutput_AgeSeconds -v`

Expected: FAIL — `unknown parse mode "age_seconds"`

- [ ] **Step 3: Implement `age_seconds` in parse.go**

Add a new case in `ParseOutput` in `internal/providersupport/probe/parse.go`, before the `default:` case (after the `regex:` case at line 69):

```go
	case mode == "age_seconds":
		s := strings.TrimSpace(stdout)
		if s == "" {
			return 0, nil
		}
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fmt.Errorf("parse age_seconds: %w", err)
		}
		return int(time.Since(ts).Seconds()), nil
```

Add `"time"` to the import block in parse.go.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/probe/ -run TestParseOutput_AgeSeconds -v`

Expected: PASS (all 3 tests)

- [ ] **Step 5: Run full probe test suite for regressions**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/probe/ -v`

Expected: All existing tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/providersupport/probe/parse.go internal/providersupport/probe/probe_test.go
git commit -m "feat: add age_seconds parse mode for timestamp-to-elapsed probes"
```

---

## Task 2: Multi-file provider loading — `LoadFromDir`

Add a `LoadFromDir` function that loads `provider.yaml` (meta/auth/variables) and merges types from `types/*.yaml`. `LoadFromFile` continues to work for single-file providers.

**Files:**
- Modify: `internal/providersupport/load.go`
- Modify: `internal/providersupport/provider_test.go`
- Create: `internal/providersupport/testdata/multifile/provider.yaml`
- Create: `internal/providersupport/testdata/multifile/types/mytype.yaml`

- [ ] **Step 1: Create multi-file test fixture**

Create `internal/providersupport/testdata/multifile/provider.yaml`:

```yaml
meta:
  name: multitest
  version: 0.1.0
  description: test provider using multi-file types

variables:
  namespace:
    description: kubernetes namespace
    required: false
    default: default

auth:
  strategy: environment
  reads_from: [KUBECONFIG]
  access:
    probes: kubectl read-only
    writes: none
```

Create `internal/providersupport/testdata/multifile/types/mytype.yaml`:

```yaml
facts:
  ready:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl get thing {name} -o jsonpath={.status.ready}"
      parse: bool
      cost: low
healthy: ["ready == true"]
states:
  missing:
    when: "ready == false"
    description: not ready
  live:
    when: "ready == true"
    description: ready
default_active_state: live
failure_modes:
  missing:
    can_cause: [upstream_failure]
```

- [ ] **Step 2: Write the failing test**

Add to `internal/providersupport/provider_test.go`:

```go
func TestLoadFromDir_MultiFile(t *testing.T) {
	p, err := LoadFromDir("testdata/multifile")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	if p.Meta.Name != "multitest" {
		t.Errorf("Meta.Name = %q, want multitest", p.Meta.Name)
	}

	if p.Variables["namespace"].Default != "default" {
		t.Errorf("namespace default = %q, want default", p.Variables["namespace"].Default)
	}

	mt, ok := p.Types["mytype"]
	if !ok {
		t.Fatal("missing type mytype")
	}

	if _, ok := mt.Facts["ready"]; !ok {
		t.Error("mytype missing fact ready")
	}

	if mt.DefaultActiveState != "live" {
		t.Errorf("DefaultActiveState = %q, want live", mt.DefaultActiveState)
	}

	if len(mt.States) != 2 {
		t.Fatalf("states count = %d, want 2", len(mt.States))
	}
	if mt.States[0].Name != "missing" {
		t.Errorf("States[0].Name = %q, want missing", mt.States[0].Name)
	}

	// Verify expressions are compiled.
	if len(mt.Healthy) != 1 || mt.Healthy[0] == nil {
		t.Error("healthy expression not compiled")
	}
	if mt.States[0].When == nil {
		t.Error("state missing.When not compiled")
	}

	causes := mt.FailureModes["missing"]
	if len(causes) != 1 || causes[0] != "upstream_failure" {
		t.Errorf("FailureModes[missing] = %v, want [upstream_failure]", causes)
	}
}

func TestLoadFromDir_FallsBackToInlineTypes(t *testing.T) {
	// LoadFromDir on a directory that has types: inline in provider.yaml
	// should still work — tests backward compatibility.
	// We'll create a test dir with a single-file provider.yaml that has types inline.
	// For simplicity, reuse the existing testdata/kubernetes.yaml by creating a temp dir.
	dir := t.TempDir()
	data, err := os.ReadFile("testdata/kubernetes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/provider.yaml", data, 0644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir with inline types: %v", err)
	}

	if p.Meta.Name != "kubernetes" {
		t.Errorf("Meta.Name = %q, want kubernetes", p.Meta.Name)
	}
	if _, ok := p.Types["deployment"]; !ok {
		t.Fatal("missing type deployment — inline types not loaded")
	}
}
```

Add `"os"` to the import block.

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/ -run "TestLoadFromDir" -v`

Expected: FAIL — `undefined: LoadFromDir`

- [ ] **Step 4: Implement `LoadFromDir`**

Add to `internal/providersupport/load.go`, after the `LoadFromFile` function (after line 169):

```go
// LoadFromDir loads a provider from a directory. It reads provider.yaml for
// meta/auth/variables/hooks. If provider.yaml contains an inline types: key,
// those are loaded (backward-compatible). Otherwise, it scans a types/
// subdirectory and loads each .yaml file as a named type.
func LoadFromDir(dir string) (*Provider, error) {
	providerPath := filepath.Join(dir, "provider.yaml")
	data, err := os.ReadFile(providerPath)
	if err != nil {
		return nil, fmt.Errorf("read provider.yaml in %q: %w", dir, err)
	}

	p, err := LoadFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse provider.yaml in %q: %w", dir, err)
	}

	// If inline types were loaded, we're done (backward-compatible).
	if len(p.Types) > 0 {
		return p, nil
	}

	// Scan types/ subdirectory.
	typesDir := filepath.Join(dir, "types")
	entries, err := os.ReadDir(typesDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No types: key and no types/ dir — valid provider with zero types.
			return p, nil
		}
		return nil, fmt.Errorf("read types dir %q: %w", typesDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		typeName := strings.TrimSuffix(entry.Name(), ".yaml")
		typeData, err := os.ReadFile(filepath.Join(typesDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read type file %q: %w", entry.Name(), err)
		}

		var typeNode yaml.Node
		if err := yaml.Unmarshal(typeData, &typeNode); err != nil {
			return nil, fmt.Errorf("type %q: YAML parse error: %w", typeName, err)
		}

		root := &typeNode
		if root.Kind == yaml.DocumentNode {
			if len(root.Content) == 0 {
				return nil, fmt.Errorf("type %q: YAML document is empty", typeName)
			}
			root = root.Content[0]
		}

		t, err := parseType(typeName, root)
		if err != nil {
			return nil, fmt.Errorf("type %q: %w", typeName, err)
		}
		p.Types[typeName] = t
	}

	return p, nil
}
```

Add `"path/filepath"` and `"strings"` to the import block in load.go. (`"strings"` may already be there — check first. `"path/filepath"` is new.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/ -run "TestLoadFromDir" -v`

Expected: PASS (both tests)

- [ ] **Step 6: Run full provider test suite for regressions**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/ -v`

Expected: All existing tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/providersupport/load.go internal/providersupport/provider_test.go internal/providersupport/testdata/multifile/
git commit -m "feat: add LoadFromDir for multi-file provider type definitions"
```

---

## Task 3: Create `providers/kubernetes/provider.yaml`

The top-level provider file with meta, auth, variables, hooks — no types.

**Files:**
- Create: `providers/kubernetes/provider.yaml`

- [ ] **Step 1: Write the failing test**

Add to `internal/providersupport/provider_test.go`:

```go
func TestLoadFromDir_KubernetesProvider(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir kubernetes: %v", err)
	}

	if p.Meta.Name != "kubernetes" {
		t.Errorf("Meta.Name = %q, want kubernetes", p.Meta.Name)
	}
	if p.Meta.Version != "2.0.0" {
		t.Errorf("Meta.Version = %q, want 2.0.0", p.Meta.Version)
	}
	if p.Variables["namespace"].Default != "default" {
		t.Errorf("namespace default = %q, want default", p.Variables["namespace"].Default)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/ -run TestLoadFromDir_KubernetesProvider -v`

Expected: FAIL — directory not found

- [ ] **Step 3: Create provider.yaml**

Create `providers/kubernetes/provider.yaml`:

```yaml
meta:
  name: kubernetes
  version: 2.0.0
  description: Kubernetes cluster resources — workloads, networking, storage, RBAC, webhooks, and scheduling
  requires:
    mgtt: ">=1.0"
  command: "$MGTT_PROVIDER_DIR/bin/mgtt-provider-kubernetes"

hooks:
  install: hooks/install.sh

variables:
  namespace:
    description: kubernetes namespace
    required: false
    default: default

auth:
  strategy: environment
  reads_from: [KUBECONFIG, ~/.kube/config, in-cluster service account]
  access:
    probes: kubectl read-only
    writes: none
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/ -run TestLoadFromDir_KubernetesProvider -v`

Expected: PASS (0 types loaded, but meta is correct)

- [ ] **Step 5: Commit**

```bash
git add providers/kubernetes/provider.yaml internal/providersupport/provider_test.go
git commit -m "feat: add kubernetes provider.yaml (meta, auth, variables)"
```

---

## Task 4: Workload types — deployment, statefulset, daemonset, replicaset

Create the 4 workload type files that model Kubernetes controller resources.

**Files:**
- Create: `providers/kubernetes/types/deployment.yaml`
- Create: `providers/kubernetes/types/statefulset.yaml`
- Create: `providers/kubernetes/types/daemonset.yaml`
- Create: `providers/kubernetes/types/replicaset.yaml`
- Modify: `internal/providersupport/provider_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/providersupport/provider_test.go`:

```go
func TestKubernetesProvider_WorkloadTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	workloads := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"deployment":  {wantFacts: 8, wantStates: 6, defaultSt: "live"},
		"statefulset": {wantFacts: 7, wantStates: 6, defaultSt: "live"},
		"daemonset":   {wantFacts: 6, wantStates: 4, defaultSt: "live"},
		"replicaset":  {wantFacts: 3, wantStates: 3, defaultSt: "live"},
	}

	for typeName, want := range workloads {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			// Verify expressions compiled.
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/ -run TestKubernetesProvider_WorkloadTypes -v`

Expected: FAIL — missing types

- [ ] **Step 3: Create deployment.yaml**

Create `providers/kubernetes/types/deployment.yaml`:

```yaml
facts:
  ready_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get deploy {name} -o jsonpath={.status.readyReplicas}"
      parse: int
      cost: low
      access: kubectl read-only
  desired_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get deploy {name} -o jsonpath={.spec.replicas}"
      parse: int
      cost: low
  updated_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get deploy {name} -o jsonpath={.status.updatedReplicas}"
      parse: int
      cost: low
  available_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get deploy {name} -o jsonpath={.status.availableReplicas}"
      parse: int
      cost: low
  restart_count:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pods -l app={name} -o jsonpath={.items[*].status.containerStatuses[0].restartCount}"
      parse: "regex:(\\d+)"
      cost: low
  unavailable_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get deploy {name} -o jsonpath={.status.unavailableReplicas}"
      parse: int
      cost: low
  condition_available:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get deploy {name} -o jsonpath={.status.conditions[?(@.type=='Available')].status}"
      parse: bool
      cost: low
  condition_progressing:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get deploy {name} -o jsonpath={.status.conditions[?(@.type=='Progressing')].status}"
      parse: bool
      cost: low

healthy:
  - ready_replicas == desired_replicas
  - condition_available == true
  - restart_count < 5

states:
  crashed:
    when: "restart_count > 5 & ready_replicas < desired_replicas"
    description: crash-looping pods
  rollout_stuck:
    when: "updated_replicas < desired_replicas & condition_progressing == false"
    description: rollout stalled
  rolling:
    when: "updated_replicas < desired_replicas"
    description: rollout in progress
  degraded:
    when: "ready_replicas < desired_replicas"
    description: some pods not ready
  scaled_to_zero:
    when: "desired_replicas == 0"
    description: intentionally drained
  live:
    when: "ready_replicas == desired_replicas"
    description: all replicas ready

default_active_state: live

failure_modes:
  crashed:
    can_cause: [upstream_failure, timeout, connection_refused, 5xx_errors]
  rollout_stuck:
    can_cause: [upstream_failure, timeout, 5xx_errors]
  rolling:
    can_cause: [upstream_failure, timeout]
  degraded:
    can_cause: [upstream_failure, timeout]
  scaled_to_zero:
    can_cause: [upstream_failure, connection_refused]
```

- [ ] **Step 4: Create statefulset.yaml**

Create `providers/kubernetes/types/statefulset.yaml`:

```yaml
facts:
  ready_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get statefulset {name} -o jsonpath={.status.readyReplicas}"
      parse: int
      cost: low
  desired_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get statefulset {name} -o jsonpath={.spec.replicas}"
      parse: int
      cost: low
  updated_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get statefulset {name} -o jsonpath={.status.updatedReplicas}"
      parse: int
      cost: low
  current_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get statefulset {name} -o jsonpath={.status.currentReplicas}"
      parse: int
      cost: low
  current_revision:
    type: mgtt.string
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get statefulset {name} -o jsonpath={.status.currentRevision}"
      parse: string
      cost: low
  update_revision:
    type: mgtt.string
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get statefulset {name} -o jsonpath={.status.updateRevision}"
      parse: string
      cost: low
  restart_count:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pods -l app={name} -o jsonpath={.items[*].status.containerStatuses[0].restartCount}"
      parse: "regex:(\\d+)"
      cost: low

healthy:
  - ready_replicas == desired_replicas
  - restart_count < 5

states:
  crashed:
    when: "restart_count > 5 & ready_replicas < desired_replicas"
    description: crash-looping pods
  rollout_stuck:
    when: "updated_replicas < desired_replicas & current_revision != update_revision"
    description: partition rollout stalled
  rolling:
    when: "current_revision != update_revision"
    description: rolling update in progress
  degraded:
    when: "ready_replicas < desired_replicas"
    description: some pods not ready
  scaled_to_zero:
    when: "desired_replicas == 0"
    description: intentionally drained
  live:
    when: "ready_replicas == desired_replicas"
    description: all replicas ready

default_active_state: live

failure_modes:
  crashed:
    can_cause: [upstream_failure, timeout, connection_refused, 5xx_errors]
  rollout_stuck:
    can_cause: [upstream_failure, timeout, 5xx_errors]
  rolling:
    can_cause: [upstream_failure, timeout]
  degraded:
    can_cause: [upstream_failure, timeout]
  scaled_to_zero:
    can_cause: [upstream_failure, connection_refused]
```

- [ ] **Step 5: Create daemonset.yaml**

Create `providers/kubernetes/types/daemonset.yaml`:

```yaml
facts:
  desired_scheduled:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get daemonset {name} -o jsonpath={.status.desiredNumberScheduled}"
      parse: int
      cost: low
  current_scheduled:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get daemonset {name} -o jsonpath={.status.currentNumberScheduled}"
      parse: int
      cost: low
  ready:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get daemonset {name} -o jsonpath={.status.numberReady}"
      parse: int
      cost: low
  misscheduled:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get daemonset {name} -o jsonpath={.status.numberMisscheduled}"
      parse: int
      cost: low
  unavailable:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get daemonset {name} -o jsonpath={.status.numberUnavailable}"
      parse: int
      cost: low
  restart_count:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pods -l app={name} -o jsonpath={.items[*].status.containerStatuses[0].restartCount}"
      parse: "regex:(\\d+)"
      cost: low

healthy:
  - ready == desired_scheduled
  - misscheduled == 0
  - restart_count < 5

states:
  crashed:
    when: "restart_count > 5 & ready < desired_scheduled"
    description: crash-looping pods
  incomplete:
    when: "ready < desired_scheduled"
    description: not running on all target nodes
  misscheduled:
    when: "misscheduled > 0"
    description: running on nodes it should not be
  live:
    when: "ready == desired_scheduled"
    description: running on all target nodes

default_active_state: live

failure_modes:
  crashed:
    can_cause: [upstream_failure, timeout, connection_refused]
  incomplete:
    can_cause: [upstream_failure, timeout]
  misscheduled:
    can_cause: [resource_contention]
```

- [ ] **Step 6: Create replicaset.yaml**

Create `providers/kubernetes/types/replicaset.yaml`:

```yaml
facts:
  ready_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get replicaset {name} -o jsonpath={.status.readyReplicas}"
      parse: int
      cost: low
  desired_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get replicaset {name} -o jsonpath={.spec.replicas}"
      parse: int
      cost: low
  available_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get replicaset {name} -o jsonpath={.status.availableReplicas}"
      parse: int
      cost: low

healthy:
  - ready_replicas == desired_replicas

states:
  degraded:
    when: "ready_replicas < desired_replicas"
    description: some pods not ready
  scaled_to_zero:
    when: "desired_replicas == 0"
    description: inactive revision
  live:
    when: "ready_replicas == desired_replicas"
    description: all replicas ready

default_active_state: live

failure_modes:
  degraded:
    can_cause: [upstream_failure, timeout]
  scaled_to_zero:
    can_cause: [upstream_failure, connection_refused]
```

- [ ] **Step 7: Run test to verify it passes**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/ -run TestKubernetesProvider_WorkloadTypes -v`

Expected: PASS (all 4 subtests)

- [ ] **Step 8: Commit**

```bash
git add providers/kubernetes/types/deployment.yaml providers/kubernetes/types/statefulset.yaml providers/kubernetes/types/daemonset.yaml providers/kubernetes/types/replicaset.yaml internal/providersupport/provider_test.go
git commit -m "feat(k8s): add workload types — deployment, statefulset, daemonset, replicaset"
```

---

## Task 5: Workload types — cronjob, job, pod

**Files:**
- Create: `providers/kubernetes/types/cronjob.yaml`
- Create: `providers/kubernetes/types/job.yaml`
- Create: `providers/kubernetes/types/pod.yaml`
- Modify: `internal/providersupport/provider_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/providersupport/provider_test.go`:

```go
func TestKubernetesProvider_ScheduledWorkloadTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"cronjob": {wantFacts: 4, wantStates: 5, defaultSt: "live"},
		"job":     {wantFacts: 6, wantStates: 4, defaultSt: "complete"},
		"pod":     {wantFacts: 6, wantStates: 6, defaultSt: "running"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/ -run TestKubernetesProvider_ScheduledWorkloadTypes -v`

Expected: FAIL — missing types

- [ ] **Step 3: Create cronjob.yaml**

Create `providers/kubernetes/types/cronjob.yaml`:

```yaml
facts:
  suspended:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get cronjob {name} -o jsonpath={.spec.suspend}"
      parse: bool
      cost: low
  active_jobs:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get cronjob {name} -o jsonpath={.status.active}"
      parse: "json:length"
      cost: low
  last_schedule_age:
    type: mgtt.int
    ttl: 30s
    description: seconds since last schedule time
    probe:
      cmd: "kubectl -n {namespace} get cronjob {name} -o jsonpath={.status.lastScheduleTime}"
      parse: age_seconds
      cost: low
  last_successful_age:
    type: mgtt.int
    ttl: 30s
    description: seconds since last successful completion
    probe:
      cmd: "kubectl -n {namespace} get cronjob {name} -o jsonpath={.status.lastSuccessfulTime}"
      parse: age_seconds
      cost: low

healthy:
  - suspended == false
  - last_successful_age < 7200

states:
  suspended:
    when: "suspended == true"
    description: manually suspended
  overdue:
    when: "last_schedule_age > 3600"
    description: has not fired in over an hour
  failing:
    when: "last_successful_age > 7200 & active_jobs == 0"
    description: recent jobs not succeeding
  active:
    when: "active_jobs > 0"
    description: job currently running
  live:
    when: "suspended == false"
    description: scheduling normally

default_active_state: live

failure_modes:
  suspended:
    can_cause: [scheduled_task_skipped]
  overdue:
    can_cause: [scheduled_task_skipped, stale_data]
  failing:
    can_cause: [scheduled_task_skipped, stale_data, data_inconsistency]
```

- [ ] **Step 4: Create job.yaml**

Create `providers/kubernetes/types/job.yaml`:

```yaml
facts:
  succeeded:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get job {name} -o jsonpath={.status.succeeded}"
      parse: int
      cost: low
  failed:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get job {name} -o jsonpath={.status.failed}"
      parse: int
      cost: low
  active:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get job {name} -o jsonpath={.status.active}"
      parse: int
      cost: low
  backoff_limit:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get job {name} -o jsonpath={.spec.backoffLimit}"
      parse: int
      cost: low
  condition_complete:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get job {name} -o jsonpath={.status.conditions[?(@.type=='Complete')].status}"
      parse: bool
      cost: low
  condition_failed:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get job {name} -o jsonpath={.status.conditions[?(@.type=='Failed')].status}"
      parse: bool
      cost: low

healthy:
  - condition_failed == false

states:
  failed:
    when: "condition_failed == true"
    description: job has permanently failed
  backoff:
    when: "failed > 0 & active == 0 & condition_complete == false"
    description: retrying after failure
  running:
    when: "active > 0"
    description: job pods executing
  complete:
    when: "condition_complete == true"
    description: job finished successfully

default_active_state: complete

failure_modes:
  failed:
    can_cause: [data_inconsistency, scheduled_task_skipped, stale_data]
  backoff:
    can_cause: [timeout, stale_data]
```

- [ ] **Step 5: Create pod.yaml**

Create `providers/kubernetes/types/pod.yaml`:

```yaml
facts:
  phase:
    type: mgtt.string
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pod {name} -o jsonpath={.status.phase}"
      parse: string
      cost: low
  ready:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pod {name} -o jsonpath={.status.conditions[?(@.type=='Ready')].status}"
      parse: bool
      cost: low
  restart_count:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pod {name} -o jsonpath={.status.containerStatuses[0].restartCount}"
      parse: int
      cost: low
  scheduled:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pod {name} -o jsonpath={.status.conditions[?(@.type=='PodScheduled')].status}"
      parse: bool
      cost: low
  containers_ready:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pod {name} -o jsonpath={.status.conditions[?(@.type=='ContainersReady')].status}"
      parse: bool
      cost: low
  oom_killed:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pod {name} -o jsonpath={.status.containerStatuses[*].lastState.terminated.reason}"
      parse: "regex:OOMKilled"
      cost: low

healthy:
  - ready == true
  - restart_count < 5

states:
  oom_killed:
    when: "oom_killed == true"
    description: container killed due to memory limit
  crash_loop:
    when: "restart_count > 5 & ready == false"
    description: container repeatedly crashing
  unschedulable:
    when: "scheduled == false"
    description: no node can accept this pod
  not_ready:
    when: "ready == false"
    description: pod exists but not serving
  running:
    when: "ready == true"
    description: pod running and ready
  succeeded:
    when: "ready == false"
    description: pod completed successfully

default_active_state: running

failure_modes:
  oom_killed:
    can_cause: [upstream_failure, timeout, connection_refused, 5xx_errors]
  crash_loop:
    can_cause: [upstream_failure, timeout, connection_refused, 5xx_errors]
  unschedulable:
    can_cause: [upstream_failure, connection_refused]
  not_ready:
    can_cause: [upstream_failure, timeout]
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/ -run TestKubernetesProvider_ScheduledWorkloadTypes -v`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add providers/kubernetes/types/cronjob.yaml providers/kubernetes/types/job.yaml providers/kubernetes/types/pod.yaml internal/providersupport/provider_test.go
git commit -m "feat(k8s): add cronjob, job, pod types"
```

---

## Task 6: Networking types — service, ingress, ingressclass, endpoints, networkpolicy

**Files:**
- Create: `providers/kubernetes/types/service.yaml`
- Create: `providers/kubernetes/types/ingress.yaml`
- Create: `providers/kubernetes/types/ingressclass.yaml`
- Create: `providers/kubernetes/types/endpoints.yaml`
- Create: `providers/kubernetes/types/networkpolicy.yaml`
- Modify: `internal/providersupport/provider_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/providersupport/provider_test.go`:

```go
func TestKubernetesProvider_NetworkingTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"service":       {wantFacts: 4, wantStates: 3, defaultSt: "live"},
		"ingress":       {wantFacts: 4, wantStates: 3, defaultSt: "live"},
		"ingressclass":  {wantFacts: 2, wantStates: 2, defaultSt: "ready"},
		"endpoints":     {wantFacts: 2, wantStates: 3, defaultSt: "live"},
		"networkpolicy": {wantFacts: 4, wantStates: 3, defaultSt: "active"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/ -run TestKubernetesProvider_NetworkingTypes -v`

Expected: FAIL — missing types

- [ ] **Step 3: Create all 5 networking type files**

Create each file with the content from the spec (section "Category: Networking" in `docs/superpowers/specs/2026-04-14-kubernetes-provider-design.md`). The exact YAML for each type is defined there — copy verbatim.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/ -run TestKubernetesProvider_NetworkingTypes -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add providers/kubernetes/types/service.yaml providers/kubernetes/types/ingress.yaml providers/kubernetes/types/ingressclass.yaml providers/kubernetes/types/endpoints.yaml providers/kubernetes/types/networkpolicy.yaml internal/providersupport/provider_test.go
git commit -m "feat(k8s): add networking types — service, ingress, ingressclass, endpoints, networkpolicy"
```

---

## Task 7: Scaling & storage types — hpa, pdb, pvc, persistentvolume, storageclass, csidriver, volumeattachment

**Files:**
- Create: `providers/kubernetes/types/hpa.yaml`
- Create: `providers/kubernetes/types/pdb.yaml`
- Create: `providers/kubernetes/types/pvc.yaml`
- Create: `providers/kubernetes/types/persistentvolume.yaml`
- Create: `providers/kubernetes/types/storageclass.yaml`
- Create: `providers/kubernetes/types/csidriver.yaml`
- Create: `providers/kubernetes/types/volumeattachment.yaml`
- Modify: `internal/providersupport/provider_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/providersupport/provider_test.go`:

```go
func TestKubernetesProvider_ScalingAndStorageTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"hpa":              {wantFacts: 8, wantStates: 4, defaultSt: "live"},
		"pdb":              {wantFacts: 4, wantStates: 3, defaultSt: "live"},
		"pvc":              {wantFacts: 4, wantStates: 3, defaultSt: "bound"},
		"persistentvolume": {wantFacts: 3, wantStates: 5, defaultSt: "bound"},
		"storageclass":     {wantFacts: 3, wantStates: 2, defaultSt: "ready"},
		"csidriver":        {wantFacts: 2, wantStates: 3, defaultSt: "ready"},
		"volumeattachment": {wantFacts: 3, wantStates: 4, defaultSt: "attached"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test, verify fail, create all 7 type files from spec, run test, verify pass**

Follow the same pattern as Tasks 4-6 — create each YAML from the spec, run the test.

- [ ] **Step 3: Commit**

```bash
git add providers/kubernetes/types/hpa.yaml providers/kubernetes/types/pdb.yaml providers/kubernetes/types/pvc.yaml providers/kubernetes/types/persistentvolume.yaml providers/kubernetes/types/storageclass.yaml providers/kubernetes/types/csidriver.yaml providers/kubernetes/types/volumeattachment.yaml internal/providersupport/provider_test.go
git commit -m "feat(k8s): add scaling and storage types — hpa, pdb, pvc, pv, storageclass, csidriver, volumeattachment"
```

---

## Task 8: Cluster & resource control types — node, resourcequota, limitrange

**Files:**
- Create: `providers/kubernetes/types/node.yaml`
- Create: `providers/kubernetes/types/resourcequota.yaml`
- Create: `providers/kubernetes/types/limitrange.yaml`
- Modify: `internal/providersupport/provider_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/providersupport/provider_test.go`:

```go
func TestKubernetesProvider_ClusterTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"node":          {wantFacts: 9, wantStates: 5, defaultSt: "ready"},
		"resourcequota": {wantFacts: 7, wantStates: 3, defaultSt: "active"},
		"limitrange":    {wantFacts: 3, wantStates: 2, defaultSt: "active"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Create all 3 type files from spec, run test, verify pass**

- [ ] **Step 3: Commit**

```bash
git add providers/kubernetes/types/node.yaml providers/kubernetes/types/resourcequota.yaml providers/kubernetes/types/limitrange.yaml internal/providersupport/provider_test.go
git commit -m "feat(k8s): add cluster types — node, resourcequota, limitrange"
```

---

## Task 9: Prerequisite types — namespace, serviceaccount, secret, configmap, operator

**Files:**
- Create: `providers/kubernetes/types/namespace.yaml`
- Create: `providers/kubernetes/types/serviceaccount.yaml`
- Create: `providers/kubernetes/types/secret.yaml`
- Create: `providers/kubernetes/types/configmap.yaml`
- Create: `providers/kubernetes/types/operator.yaml`
- Modify: `internal/providersupport/provider_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/providersupport/provider_test.go`:

```go
func TestKubernetesProvider_PrerequisiteTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"namespace":      {wantFacts: 2, wantStates: 3, defaultSt: "active"},
		"serviceaccount": {wantFacts: 3, wantStates: 3, defaultSt: "ready"},
		"secret":         {wantFacts: 3, wantStates: 3, defaultSt: "ready"},
		"configmap":      {wantFacts: 3, wantStates: 3, defaultSt: "ready"},
		"operator":       {wantFacts: 4, wantStates: 4, defaultSt: "ready"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Create all 5 type files from spec, run test, verify pass**

- [ ] **Step 3: Commit**

```bash
git add providers/kubernetes/types/namespace.yaml providers/kubernetes/types/serviceaccount.yaml providers/kubernetes/types/secret.yaml providers/kubernetes/types/configmap.yaml providers/kubernetes/types/operator.yaml internal/providersupport/provider_test.go
git commit -m "feat(k8s): add prerequisite types — namespace, serviceaccount, secret, configmap, operator"
```

---

## Task 10: RBAC types — role, clusterrole, rolebinding, clusterrolebinding

**Files:**
- Create: `providers/kubernetes/types/role.yaml`
- Create: `providers/kubernetes/types/clusterrole.yaml`
- Create: `providers/kubernetes/types/rolebinding.yaml`
- Create: `providers/kubernetes/types/clusterrolebinding.yaml`
- Modify: `internal/providersupport/provider_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/providersupport/provider_test.go`:

```go
func TestKubernetesProvider_RBACTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"role":               {wantFacts: 2, wantStates: 2, defaultSt: "ready"},
		"clusterrole":        {wantFacts: 2, wantStates: 2, defaultSt: "ready"},
		"rolebinding":        {wantFacts: 3, wantStates: 3, defaultSt: "ready"},
		"clusterrolebinding": {wantFacts: 3, wantStates: 3, defaultSt: "ready"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Create all 4 type files from spec, run test, verify pass**

- [ ] **Step 3: Commit**

```bash
git add providers/kubernetes/types/role.yaml providers/kubernetes/types/clusterrole.yaml providers/kubernetes/types/rolebinding.yaml providers/kubernetes/types/clusterrolebinding.yaml internal/providersupport/provider_test.go
git commit -m "feat(k8s): add RBAC types — role, clusterrole, rolebinding, clusterrolebinding"
```

---

## Task 11: Webhook types — validatingwebhookconfiguration, mutatingwebhookconfiguration

**Files:**
- Create: `providers/kubernetes/types/validatingwebhookconfiguration.yaml`
- Create: `providers/kubernetes/types/mutatingwebhookconfiguration.yaml`
- Modify: `internal/providersupport/provider_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/providersupport/provider_test.go`:

```go
func TestKubernetesProvider_WebhookTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"validatingwebhookconfiguration": {wantFacts: 4, wantStates: 4, defaultSt: "active"},
		"mutatingwebhookconfiguration":   {wantFacts: 4, wantStates: 4, defaultSt: "active"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Create both type files from spec, run test, verify pass**

- [ ] **Step 3: Commit**

```bash
git add providers/kubernetes/types/validatingwebhookconfiguration.yaml providers/kubernetes/types/mutatingwebhookconfiguration.yaml internal/providersupport/provider_test.go
git commit -m "feat(k8s): add webhook types — validating and mutating webhook configurations"
```

---

## Task 12: API, scheduling & extensibility types — customresourcedefinition, priorityclass, lease, custom_resource

**Files:**
- Create: `providers/kubernetes/types/customresourcedefinition.yaml`
- Create: `providers/kubernetes/types/priorityclass.yaml`
- Create: `providers/kubernetes/types/lease.yaml`
- Create: `providers/kubernetes/types/custom_resource.yaml`
- Modify: `internal/providersupport/provider_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/providersupport/provider_test.go`:

```go
func TestKubernetesProvider_ExtensibilityTypes(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	types := map[string]struct {
		wantFacts  int
		wantStates int
		defaultSt  string
	}{
		"customresourcedefinition": {wantFacts: 3, wantStates: 4, defaultSt: "ready"},
		"priorityclass":           {wantFacts: 3, wantStates: 2, defaultSt: "ready"},
		"lease":                   {wantFacts: 4, wantStates: 3, defaultSt: "held"},
		"custom_resource":         {wantFacts: 4, wantStates: 4, defaultSt: "ready"},
	}

	for typeName, want := range types {
		t.Run(typeName, func(t *testing.T) {
			typ, ok := p.Types[typeName]
			if !ok {
				t.Fatalf("missing type %s", typeName)
			}
			if len(typ.Facts) != want.wantFacts {
				t.Errorf("facts count = %d, want %d", len(typ.Facts), want.wantFacts)
			}
			if len(typ.States) != want.wantStates {
				t.Errorf("states count = %d, want %d", len(typ.States), want.wantStates)
			}
			if typ.DefaultActiveState != want.defaultSt {
				t.Errorf("default_active_state = %q, want %q", typ.DefaultActiveState, want.defaultSt)
			}
			for _, h := range typ.Healthy {
				if h == nil {
					t.Error("nil healthy expression")
				}
			}
			for _, s := range typ.States {
				if s.WhenRaw != "" && s.When == nil {
					t.Errorf("state %q: When not compiled", s.Name)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Create all 4 type files from spec, run test, verify pass**

- [ ] **Step 3: Commit**

```bash
git add providers/kubernetes/types/customresourcedefinition.yaml providers/kubernetes/types/priorityclass.yaml providers/kubernetes/types/lease.yaml providers/kubernetes/types/custom_resource.yaml internal/providersupport/provider_test.go
git commit -m "feat(k8s): add extensibility types — CRD, priorityclass, lease, custom_resource"
```

---

## Task 13: Full provider integration test — load all 37 types and verify total

**Files:**
- Modify: `internal/providersupport/provider_test.go`

- [ ] **Step 1: Write the integration test**

Add to `internal/providersupport/provider_test.go`:

```go
func TestKubernetesProvider_FullLoad(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	if p.Meta.Name != "kubernetes" {
		t.Errorf("Meta.Name = %q, want kubernetes", p.Meta.Name)
	}

	// Verify all 37 types loaded.
	expectedTypes := []string{
		// Workloads
		"deployment", "statefulset", "daemonset", "replicaset", "cronjob", "job", "pod",
		// Networking
		"service", "ingress", "ingressclass", "endpoints", "networkpolicy",
		// Scaling & availability
		"hpa", "pdb",
		// Storage
		"pvc", "persistentvolume", "storageclass", "csidriver", "volumeattachment",
		// Cluster
		"node",
		// Resource control
		"resourcequota", "limitrange",
		// Prerequisites
		"namespace", "serviceaccount", "secret", "configmap", "operator",
		// RBAC
		"role", "clusterrole", "rolebinding", "clusterrolebinding",
		// Webhooks
		"validatingwebhookconfiguration", "mutatingwebhookconfiguration",
		// API / scheduling / extensibility
		"customresourcedefinition", "priorityclass", "lease", "custom_resource",
	}

	if len(p.Types) != len(expectedTypes) {
		t.Errorf("type count = %d, want %d", len(p.Types), len(expectedTypes))
	}

	for _, name := range expectedTypes {
		typ, ok := p.Types[name]
		if !ok {
			t.Errorf("missing type %q", name)
			continue
		}
		// Every type must have at least 1 fact, 1 state, and a default_active_state.
		if len(typ.Facts) == 0 {
			t.Errorf("type %q has no facts", name)
		}
		if len(typ.States) == 0 {
			t.Errorf("type %q has no states", name)
		}
		if typ.DefaultActiveState == "" {
			t.Errorf("type %q has no default_active_state", name)
		}
		// All healthy expressions must be compiled.
		for i, h := range typ.Healthy {
			if h == nil {
				t.Errorf("type %q: Healthy[%d] not compiled", name, i)
			}
		}
		// All state when-expressions must be compiled.
		for _, s := range typ.States {
			if s.WhenRaw != "" && s.When == nil {
				t.Errorf("type %q: state %q When not compiled", name, s.Name)
			}
		}
	}
}

func TestKubernetesProvider_RegistryIntegration(t *testing.T) {
	p, err := LoadFromDir("../../providers/kubernetes")
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	reg := NewRegistry()
	reg.Register(p)

	// Resolve a sampling of types.
	samples := []string{"deployment", "service", "hpa", "node", "secret", "rolebinding", "custom_resource"}
	for _, typeName := range samples {
		typ, provName, err := reg.ResolveType([]string{"kubernetes"}, typeName)
		if err != nil {
			t.Errorf("ResolveType %q: %v", typeName, err)
			continue
		}
		if provName != "kubernetes" {
			t.Errorf("ResolveType %q: provider = %q, want kubernetes", typeName, provName)
		}
		if typ.Name != typeName {
			t.Errorf("ResolveType %q: type.Name = %q", typeName, typ.Name)
		}
	}
}
```

- [ ] **Step 2: Run both tests**

Run: `cd /root/docs/projects/mgtt && go test ./internal/providersupport/ -run "TestKubernetesProvider_FullLoad|TestKubernetesProvider_RegistryIntegration" -v`

Expected: PASS

- [ ] **Step 3: Run the full test suite**

Run: `cd /root/docs/projects/mgtt && go test ./... -v`

Expected: All tests PASS (existing tests for single-file loading, registry, probe parsing, simulation all still work)

- [ ] **Step 4: Commit**

```bash
git add internal/providersupport/provider_test.go
git commit -m "test: add full integration test for 37-type kubernetes provider"
```

---

## Task 14: Update providers/README.md with multi-file documentation

**Files:**
- Modify: `providers/README.md`

- [ ] **Step 1: Add multi-file provider section**

Add a new section to `providers/README.md` after the existing directory structure section, documenting:

1. Multi-file provider structure: `provider.yaml` + `types/*.yaml`
2. How the filename becomes the type name
3. Backward compatibility: single-file providers with inline `types:` still work
4. Example showing both formats

- [ ] **Step 2: Commit**

```bash
git add providers/README.md
git commit -m "docs: document multi-file provider structure in providers/README.md"
```
