# Kubernetes Provider — Exhaustive Design

**Date:** 2026-04-14
**Status:** Draft
**Scope:** Extend the mgtt Kubernetes provider from 2 types to 37, covering every K8s resource with meaningful troubleshooting state. Refactor provider loading to support multi-file type definitions.

## Motivation

The current Kubernetes provider has only `deployment` and `ingress` — enough for simulation tests but not for real clusters. A production K8s setup (e.g., a Magento e-commerce platform on EKS) involves Deployments, StatefulSets, Services, HPAs, PVCs, RBAC, Webhooks, Operators, and connections to external managed services. Users can't model these systems without a provider that covers the full Kubernetes vocabulary.

The AWS provider will handle managed services (RDS, ElastiCache, SQS, etc.) separately — mgtt's multi-provider model design supports `providers: [kubernetes, aws]` natively.

## Design Decisions

1. **Multi-file provider structure** — split `types:` out of `provider.yaml` into individual `types/<name>.yaml` files. Backward-compatible: single-file providers with inline `types:` still work.

2. **37 types** — exhaustive coverage of K8s resources that have runtime troubleshooting state. RBAC and webhooks included because they are root causes of failures, not just prerequisites.

3. **No application-level types** — nginx, php-fpm, Redis, etc. are applications running as Deployments. The K8s provider models infrastructure primitives; the model author names their components and wires dependencies.

4. **Probe commands use kubectl** — all probes are `kubectl get ... -o jsonpath` or equivalent. No in-cluster agent required. Read-only access.

5. **Variables** — each type uses `{namespace}` (from provider-level default) and `{name}` (from component-level `vars`). Some types use additional variables documented per-type.

## Provider Structure

### Directory Layout

```
providers/kubernetes/
  provider.yaml              # meta, auth, variables, hooks — no types
  types/
    deployment.yaml
    statefulset.yaml
    daemonset.yaml
    replicaset.yaml
    cronjob.yaml
    job.yaml
    pod.yaml
    service.yaml
    ingress.yaml
    ingressclass.yaml
    endpoints.yaml
    networkpolicy.yaml
    hpa.yaml
    pdb.yaml
    pvc.yaml
    persistentvolume.yaml
    storageclass.yaml
    csidriver.yaml
    volumeattachment.yaml
    node.yaml
    resourcequota.yaml
    limitrange.yaml
    namespace.yaml
    serviceaccount.yaml
    secret.yaml
    configmap.yaml
    operator.yaml
    role.yaml
    clusterrole.yaml
    rolebinding.yaml
    clusterrolebinding.yaml
    validatingwebhookconfiguration.yaml
    mutatingwebhookconfiguration.yaml
    customresourcedefinition.yaml
    priorityclass.yaml
    lease.yaml
    custom_resource.yaml
```

### provider.yaml

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

### Loading Mechanism Change

When `load.go` parses a provider directory:

1. Read `provider.yaml` — extract meta, auth, variables, hooks.
2. If `provider.yaml` contains a `types:` key, load types inline (current behavior, backward-compatible).
3. If no `types:` key, look for a `types/` subdirectory alongside `provider.yaml`.
4. For each `.yaml` file in `types/`, parse it as a single type definition. The filename (minus `.yaml`) becomes the type name.
5. Merge all types into the provider's `Types` map.

Each type file has the same structure as what currently goes under `types.<name>:` — facts, healthy, states, default_active_state, failure_modes.

## Type Definitions

### Category: Workloads

#### deployment

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

#### statefulset

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

#### daemonset

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

#### replicaset

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

#### cronjob

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

#### job

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

#### pod

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
    when: "phase == \"Running\" & ready == true"
    description: pod running and ready
  succeeded:
    when: "phase == \"Succeeded\""
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

### Category: Networking

#### service

```yaml
facts:
  endpoint_count:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get endpoints {name} -o jsonpath={.subsets[0].addresses}"
      parse: "json:length"
      cost: low
  type:
    type: mgtt.string
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get svc {name} -o jsonpath={.spec.type}"
      parse: string
      cost: low
  selector_match:
    type: mgtt.bool
    ttl: 30s
    description: whether selector matches any running pods
    probe:
      cmd: "kubectl -n {namespace} get endpoints {name} -o jsonpath={.subsets[0].addresses[0].ip}"
      parse: "regex:.+"
      cost: low
  external_ip_assigned:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get svc {name} -o jsonpath={.status.loadBalancer.ingress[0]}"
      parse: "regex:.+"
      cost: low

healthy:
  - endpoint_count > 0

states:
  no_endpoints:
    when: "endpoint_count == 0"
    description: selector matches nothing
  pending_lb:
    when: "type == \"LoadBalancer\" & external_ip_assigned == false"
    description: load balancer not yet provisioned
  live:
    when: "endpoint_count > 0"
    description: routing traffic

default_active_state: live

failure_modes:
  no_endpoints:
    can_cause: [upstream_failure, connection_refused, dns_failure]
  pending_lb:
    can_cause: [upstream_failure, connection_refused, timeout]
```

#### ingress

```yaml
facts:
  backend_count:
    type: mgtt.int
    ttl: 30s
    description: number of backends with healthy endpoints
    probe:
      cmd: "kubectl -n {namespace} get ingress {name} -o json"
      parse: "json:.spec.rules[*].http.paths | length"
      cost: low
  address_assigned:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get ingress {name} -o jsonpath={.status.loadBalancer.ingress[0]}"
      parse: "regex:.+"
      cost: low
  tls_configured:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get ingress {name} -o jsonpath={.spec.tls}"
      parse: "regex:.+"
      cost: low
  class:
    type: mgtt.string
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get ingress {name} -o jsonpath={.spec.ingressClassName}"
      parse: string
      cost: low

healthy:
  - address_assigned == true
  - backend_count > 0

states:
  no_backends:
    when: "backend_count == 0"
    description: no backend services configured
  no_address:
    when: "address_assigned == false"
    description: load balancer not provisioned
  live:
    when: "address_assigned == true & backend_count > 0"
    description: serving traffic

default_active_state: live

failure_modes:
  no_backends:
    can_cause: [upstream_failure, 5xx_errors]
  no_address:
    can_cause: [upstream_failure, connection_refused, dns_failure]
```

#### ingressclass

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get ingressclass {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  controller:
    type: mgtt.string
    ttl: 60s
    probe:
      cmd: "kubectl get ingressclass {name} -o jsonpath={.spec.controller}"
      parse: string
      cost: low

healthy:
  - exists == true

states:
  missing:
    when: "exists == false"
    description: ingress class not registered
  ready:
    when: "exists == true"
    description: ingress class available

default_active_state: ready

failure_modes:
  missing:
    can_cause: [upstream_failure, connection_refused]
```

#### endpoints

```yaml
facts:
  ready_count:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get endpoints {name} -o jsonpath={.subsets[0].addresses}"
      parse: "json:length"
      cost: low
  not_ready_count:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get endpoints {name} -o jsonpath={.subsets[0].notReadyAddresses}"
      parse: "json:length"
      cost: low

healthy:
  - ready_count > 0

states:
  empty:
    when: "ready_count == 0"
    description: no ready addresses
  partial:
    when: "not_ready_count > 0 & ready_count > 0"
    description: some addresses not ready
  live:
    when: "ready_count > 0 & not_ready_count == 0"
    description: all addresses ready

default_active_state: live

failure_modes:
  empty:
    can_cause: [upstream_failure, connection_refused]
  partial:
    can_cause: [upstream_failure, timeout]
```

#### networkpolicy

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get networkpolicy {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  pod_selector_match_count:
    type: mgtt.int
    ttl: 30s
    description: number of pods matching the policy selector
    probe:
      cmd: "kubectl -n {namespace} get networkpolicy {name} -o jsonpath={.spec.podSelector.matchLabels}"
      parse: "json:length"
      cost: medium
  ingress_rule_count:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get networkpolicy {name} -o jsonpath={.spec.ingress}"
      parse: "json:length"
      cost: low
  egress_rule_count:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get networkpolicy {name} -o jsonpath={.spec.egress}"
      parse: "json:length"
      cost: low

healthy:
  - exists == true

states:
  missing:
    when: "exists == false"
    description: network policy does not exist
  no_targets:
    when: "pod_selector_match_count == 0"
    description: policy targets no pods
  active:
    when: "exists == true & pod_selector_match_count > 0"
    description: policy applied to pods

default_active_state: active

failure_modes:
  missing:
    can_cause: [security_violation]
  no_targets:
    can_cause: [security_violation]
```

### Category: Scaling & Availability

#### hpa

```yaml
facts:
  current_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get hpa {name} -o jsonpath={.status.currentReplicas}"
      parse: int
      cost: low
  desired_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get hpa {name} -o jsonpath={.status.desiredReplicas}"
      parse: int
      cost: low
  min_replicas:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get hpa {name} -o jsonpath={.spec.minReplicas}"
      parse: int
      cost: low
  max_replicas:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get hpa {name} -o jsonpath={.spec.maxReplicas}"
      parse: int
      cost: low
  cpu_current:
    type: mgtt.percentage
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get hpa {name} -o jsonpath={.status.currentMetrics[?(@.type=='Resource')].resource.current.averageUtilization}"
      parse: int
      cost: low
  cpu_target:
    type: mgtt.percentage
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get hpa {name} -o jsonpath={.spec.metrics[?(@.type=='Resource')].resource.target.averageUtilization}"
      parse: int
      cost: low
  scaling_active:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get hpa {name} -o jsonpath={.status.conditions[?(@.type=='ScalingActive')].status}"
      parse: bool
      cost: low
  able_to_scale:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get hpa {name} -o jsonpath={.status.conditions[?(@.type=='AbleToScale')].status}"
      parse: bool
      cost: low

healthy:
  - able_to_scale == true
  - scaling_active == true

states:
  unable_to_scale:
    when: "able_to_scale == false"
    description: HPA cannot adjust replicas
  at_max:
    when: "current_replicas == max_replicas & cpu_current > cpu_target"
    description: at maximum replicas but still over target
  scaling:
    when: "current_replicas != desired_replicas"
    description: scaling in progress
  live:
    when: "scaling_active == true & able_to_scale == true"
    description: autoscaler operating normally

default_active_state: live

failure_modes:
  unable_to_scale:
    can_cause: [upstream_failure, timeout, 5xx_errors, resource_contention]
  at_max:
    can_cause: [timeout, 5xx_errors, resource_contention]
  scaling:
    can_cause: [timeout]
```

#### pdb

```yaml
facts:
  allowed_disruptions:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pdb {name} -o jsonpath={.status.disruptionsAllowed}"
      parse: int
      cost: low
  current_healthy:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pdb {name} -o jsonpath={.status.currentHealthy}"
      parse: int
      cost: low
  desired_healthy:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pdb {name} -o jsonpath={.status.desiredHealthy}"
      parse: int
      cost: low
  expected_pods:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pdb {name} -o jsonpath={.status.expectedPods}"
      parse: int
      cost: low

healthy:
  - allowed_disruptions > 0

states:
  blocked:
    when: "allowed_disruptions == 0 & current_healthy <= desired_healthy"
    description: no disruptions allowed — blocks node drains
  violated:
    when: "current_healthy < desired_healthy"
    description: fewer healthy pods than minimum
  live:
    when: "allowed_disruptions > 0"
    description: disruption budget satisfied

default_active_state: live

failure_modes:
  blocked:
    can_cause: [deployment_blocked, node_drain_stuck]
  violated:
    can_cause: [upstream_failure, timeout]
```

### Category: Storage

#### pvc

```yaml
facts:
  phase:
    type: mgtt.string
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get pvc {name} -o jsonpath={.status.phase}"
      parse: string
      cost: low
  capacity:
    type: mgtt.int
    ttl: 60s
    description: capacity in bytes
    probe:
      cmd: "kubectl -n {namespace} get pvc {name} -o jsonpath={.status.capacity.storage}"
      parse: int
      cost: low
  storage_class:
    type: mgtt.string
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get pvc {name} -o jsonpath={.spec.storageClassName}"
      parse: string
      cost: low
  volume_name:
    type: mgtt.string
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get pvc {name} -o jsonpath={.spec.volumeName}"
      parse: string
      cost: low

healthy:
  - phase == "Bound"

states:
  lost:
    when: "phase == \"Lost\""
    description: underlying volume lost
  pending:
    when: "phase == \"Pending\""
    description: waiting for volume provisioning
  bound:
    when: "phase == \"Bound\""
    description: volume bound and available

default_active_state: bound

failure_modes:
  lost:
    can_cause: [data_loss, upstream_failure, connection_refused]
  pending:
    can_cause: [upstream_failure, timeout]
```

#### persistentvolume

```yaml
facts:
  phase:
    type: mgtt.string
    ttl: 30s
    probe:
      cmd: "kubectl get pv {name} -o jsonpath={.status.phase}"
      parse: string
      cost: low
  capacity:
    type: mgtt.int
    ttl: 60s
    description: capacity in bytes
    probe:
      cmd: "kubectl get pv {name} -o jsonpath={.spec.capacity.storage}"
      parse: int
      cost: low
  reclaim_policy:
    type: mgtt.string
    ttl: 60s
    probe:
      cmd: "kubectl get pv {name} -o jsonpath={.spec.persistentVolumeReclaimPolicy}"
      parse: string
      cost: low

healthy:
  - phase == "Bound"

states:
  failed:
    when: "phase == \"Failed\""
    description: reclamation failed
  released:
    when: "phase == \"Released\""
    description: claim deleted, not yet reclaimed
  pending:
    when: "phase == \"Pending\""
    description: not yet available
  available:
    when: "phase == \"Available\""
    description: unbound, available for claims
  bound:
    when: "phase == \"Bound\""
    description: bound to a claim

default_active_state: bound

failure_modes:
  failed:
    can_cause: [data_loss, upstream_failure]
  released:
    can_cause: [upstream_failure]
  pending:
    can_cause: [upstream_failure, timeout]
```

#### storageclass

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get storageclass {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  provisioner:
    type: mgtt.string
    ttl: 60s
    probe:
      cmd: "kubectl get storageclass {name} -o jsonpath={.provisioner}"
      parse: string
      cost: low
  is_default:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get storageclass {name} -o jsonpath={.metadata.annotations.storageclass\\.kubernetes\\.io/is-default-class}"
      parse: bool
      cost: low

healthy:
  - exists == true

states:
  missing:
    when: "exists == false"
    description: storage class not registered
  ready:
    when: "exists == true"
    description: storage class available

default_active_state: ready

failure_modes:
  missing:
    can_cause: [upstream_failure, timeout]
```

#### csidriver

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get csidriver {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  node_count:
    type: mgtt.int
    ttl: 60s
    description: number of CSINode objects referencing this driver
    probe:
      cmd: "kubectl get csinodes -o json"
      parse: "json:.items[*].spec.drivers[?(@.name=='{name}')] | length"
      cost: medium

healthy:
  - exists == true
  - node_count > 0

states:
  missing:
    when: "exists == false"
    description: CSI driver not registered
  no_nodes:
    when: "exists == true & node_count == 0"
    description: driver registered but not running on any node
  ready:
    when: "exists == true & node_count > 0"
    description: driver active on nodes

default_active_state: ready

failure_modes:
  missing:
    can_cause: [upstream_failure, timeout]
  no_nodes:
    can_cause: [upstream_failure, timeout]
```

#### volumeattachment

```yaml
facts:
  attached:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl get volumeattachment {name} -o jsonpath={.status.attached}"
      parse: bool
      cost: low
  attach_error:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl get volumeattachment {name} -o jsonpath={.status.attachError.message}"
      parse: "regex:.+"
      cost: low
  age:
    type: mgtt.int
    ttl: 30s
    description: seconds since creation
    probe:
      cmd: "kubectl get volumeattachment {name} -o jsonpath={.metadata.creationTimestamp}"
      parse: age_seconds
      cost: low

healthy:
  - attached == true

states:
  error:
    when: "attach_error == true"
    description: volume attachment failed with error
  stuck:
    when: "attached == false & age > 300"
    description: attachment taking too long
  attaching:
    when: "attached == false"
    description: attachment in progress
  attached:
    when: "attached == true"
    description: volume attached to node

default_active_state: attached

failure_modes:
  error:
    can_cause: [upstream_failure, data_loss]
  stuck:
    can_cause: [upstream_failure, timeout]
  attaching:
    can_cause: [timeout]
```

### Category: Cluster

#### node

```yaml
facts:
  ready:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl get node {name} -o jsonpath={.status.conditions[?(@.type=='Ready')].status}"
      parse: bool
      cost: low
  memory_pressure:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl get node {name} -o jsonpath={.status.conditions[?(@.type=='MemoryPressure')].status}"
      parse: bool
      cost: low
  disk_pressure:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl get node {name} -o jsonpath={.status.conditions[?(@.type=='DiskPressure')].status}"
      parse: bool
      cost: low
  pid_pressure:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl get node {name} -o jsonpath={.status.conditions[?(@.type=='PIDPressure')].status}"
      parse: bool
      cost: low
  network_unavailable:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl get node {name} -o jsonpath={.status.conditions[?(@.type=='NetworkUnavailable')].status}"
      parse: bool
      cost: low
  unschedulable:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl get node {name} -o jsonpath={.spec.unschedulable}"
      parse: bool
      cost: low
  cpu_allocatable:
    type: mgtt.int
    ttl: 60s
    description: allocatable CPU in millicores
    probe:
      cmd: "kubectl get node {name} -o jsonpath={.status.allocatable.cpu}"
      parse: int
      cost: low
  memory_allocatable:
    type: mgtt.int
    ttl: 60s
    description: allocatable memory in bytes
    probe:
      cmd: "kubectl get node {name} -o jsonpath={.status.allocatable.memory}"
      parse: int
      cost: low
  pod_count:
    type: mgtt.int
    ttl: 30s
    description: number of pods on this node
    probe:
      cmd: "kubectl get pods --all-namespaces --field-selector spec.nodeName={name} -o json"
      parse: "json:.items | length"
      cost: medium

healthy:
  - ready == true
  - memory_pressure == false
  - disk_pressure == false

states:
  not_ready:
    when: "ready == false"
    description: node not ready
  cordoned:
    when: "unschedulable == true"
    description: node cordoned for maintenance
  pressure:
    when: "memory_pressure == true | disk_pressure == true | pid_pressure == true"
    description: node under resource pressure
  network_down:
    when: "network_unavailable == true"
    description: node network plugin not ready
  ready:
    when: "ready == true"
    description: node healthy and schedulable

default_active_state: ready

failure_modes:
  not_ready:
    can_cause: [upstream_failure, timeout, connection_refused]
  cordoned:
    can_cause: [upstream_failure, resource_contention]
  pressure:
    can_cause: [upstream_failure, timeout, resource_contention]
  network_down:
    can_cause: [upstream_failure, connection_refused, timeout]
```

### Category: Resource Control

#### resourcequota

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get resourcequota {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  cpu_used:
    type: mgtt.int
    ttl: 30s
    description: CPU used in millicores
    probe:
      cmd: "kubectl -n {namespace} get resourcequota {name} -o jsonpath={.status.used.cpu}"
      parse: int
      cost: low
  cpu_hard:
    type: mgtt.int
    ttl: 60s
    description: CPU limit in millicores
    probe:
      cmd: "kubectl -n {namespace} get resourcequota {name} -o jsonpath={.status.hard.cpu}"
      parse: int
      cost: low
  memory_used:
    type: mgtt.int
    ttl: 30s
    description: memory used in bytes
    probe:
      cmd: "kubectl -n {namespace} get resourcequota {name} -o jsonpath={.status.used.memory}"
      parse: int
      cost: low
  memory_hard:
    type: mgtt.int
    ttl: 60s
    description: memory limit in bytes
    probe:
      cmd: "kubectl -n {namespace} get resourcequota {name} -o jsonpath={.status.hard.memory}"
      parse: int
      cost: low
  pods_used:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get resourcequota {name} -o jsonpath={.status.used.pods}"
      parse: int
      cost: low
  pods_hard:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get resourcequota {name} -o jsonpath={.status.hard.pods}"
      parse: int
      cost: low

healthy:
  - pods_used < pods_hard
  - cpu_used < cpu_hard

states:
  missing:
    when: "exists == false"
    description: quota not defined
  exhausted:
    when: "pods_used >= pods_hard | cpu_used >= cpu_hard"
    description: quota fully consumed — new pods will be rejected
  active:
    when: "exists == true"
    description: quota in effect with headroom

default_active_state: active

failure_modes:
  exhausted:
    can_cause: [upstream_failure, deployment_blocked, resource_contention]
```

#### limitrange

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get limitrange {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  default_cpu_limit:
    type: mgtt.int
    ttl: 60s
    description: default CPU limit in millicores
    probe:
      cmd: "kubectl -n {namespace} get limitrange {name} -o jsonpath={.spec.limits[0].default.cpu}"
      parse: int
      cost: low
  default_memory_limit:
    type: mgtt.int
    ttl: 60s
    description: default memory limit in bytes
    probe:
      cmd: "kubectl -n {namespace} get limitrange {name} -o jsonpath={.spec.limits[0].default.memory}"
      parse: int
      cost: low

healthy:
  - exists == true

states:
  missing:
    when: "exists == false"
    description: limit range not defined
  active:
    when: "exists == true"
    description: limit range in effect

default_active_state: active

failure_modes:
  missing:
    can_cause: [resource_contention]
```

### Category: Prerequisites

#### namespace

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get namespace {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  phase:
    type: mgtt.string
    ttl: 30s
    probe:
      cmd: "kubectl get namespace {name} -o jsonpath={.status.phase}"
      parse: string
      cost: low

healthy:
  - phase == "Active"

states:
  terminating:
    when: "phase == \"Terminating\""
    description: namespace being deleted
  missing:
    when: "exists == false"
    description: namespace does not exist
  active:
    when: "phase == \"Active\""
    description: namespace active

default_active_state: active

failure_modes:
  terminating:
    can_cause: [upstream_failure, deployment_blocked]
  missing:
    can_cause: [upstream_failure, deployment_blocked]
```

#### serviceaccount

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get serviceaccount {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  irsa_role_arn:
    type: mgtt.string
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get serviceaccount {name} -o jsonpath={.metadata.annotations.eks\\.amazonaws\\.com/role-arn}"
      parse: string
      cost: low
  has_irsa:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get serviceaccount {name} -o jsonpath={.metadata.annotations.eks\\.amazonaws\\.com/role-arn}"
      parse: "regex:.+"
      cost: low

healthy:
  - exists == true

states:
  missing:
    when: "exists == false"
    description: service account does not exist
  no_irsa:
    when: "exists == true & has_irsa == false"
    description: no IAM role annotation — pods cannot assume AWS roles
  ready:
    when: "exists == true"
    description: service account available

default_active_state: ready

failure_modes:
  missing:
    can_cause: [upstream_failure, permission_denied, deployment_blocked]
  no_irsa:
    can_cause: [permission_denied]
```

#### secret

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get secret {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  key_count:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get secret {name} -o json"
      parse: "json:.data | length"
      cost: low
  age:
    type: mgtt.int
    ttl: 60s
    description: seconds since creation
    probe:
      cmd: "kubectl -n {namespace} get secret {name} -o jsonpath={.metadata.creationTimestamp}"
      parse: age_seconds
      cost: low

healthy:
  - exists == true
  - key_count > 0

states:
  missing:
    when: "exists == false"
    description: secret does not exist
  empty:
    when: "exists == true & key_count == 0"
    description: secret exists but has no data keys
  ready:
    when: "exists == true & key_count > 0"
    description: secret available with data

default_active_state: ready

failure_modes:
  missing:
    can_cause: [upstream_failure, deployment_blocked, connection_refused]
  empty:
    can_cause: [upstream_failure, connection_refused]
```

#### configmap

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get configmap {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  key_count:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get configmap {name} -o json"
      parse: "json:.data | length"
      cost: low
  age:
    type: mgtt.int
    ttl: 60s
    description: seconds since creation
    probe:
      cmd: "kubectl -n {namespace} get configmap {name} -o jsonpath={.metadata.creationTimestamp}"
      parse: age_seconds
      cost: low

healthy:
  - exists == true
  - key_count > 0

states:
  missing:
    when: "exists == false"
    description: configmap does not exist
  empty:
    when: "exists == true & key_count == 0"
    description: configmap exists but has no data keys
  ready:
    when: "exists == true & key_count > 0"
    description: configmap available with data

default_active_state: ready

failure_modes:
  missing:
    can_cause: [upstream_failure, deployment_blocked]
  empty:
    can_cause: [upstream_failure]
```

#### operator

```yaml
# Variables: name (operator deployment name), crd_name (expected CRD), operator_namespace (defaults to {namespace})
facts:
  deployment_ready:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {operator_namespace} get deploy {name} -o jsonpath={.status.conditions[?(@.type=='Available')].status}"
      parse: bool
      cost: low
  crd_registered:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get crd {crd_name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  webhook_healthy:
    type: mgtt.bool
    ttl: 30s
    description: whether operator webhook service has endpoints
    probe:
      cmd: "kubectl -n {operator_namespace} get endpoints {name}-webhook-service -o jsonpath={.subsets[0].addresses[0].ip}"
      parse: "regex:.+"
      cost: medium
  restart_count:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {operator_namespace} get pods -l app={name} -o jsonpath={.items[0].status.containerStatuses[0].restartCount}"
      parse: int
      cost: low

healthy:
  - deployment_ready == true
  - crd_registered == true

states:
  crd_missing:
    when: "crd_registered == false"
    description: custom resource definition not registered
  not_running:
    when: "deployment_ready == false"
    description: operator deployment not available
  degraded:
    when: "restart_count > 3"
    description: operator pod restarting frequently
  ready:
    when: "deployment_ready == true & crd_registered == true"
    description: operator running and CRD registered

default_active_state: ready

failure_modes:
  crd_missing:
    can_cause: [upstream_failure, deployment_blocked]
  not_running:
    can_cause: [upstream_failure, deployment_blocked]
  degraded:
    can_cause: [upstream_failure, timeout]
```

### Category: RBAC

#### role

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get role {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  rule_count:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get role {name} -o json"
      parse: "json:.rules | length"
      cost: low

healthy:
  - exists == true

states:
  missing:
    when: "exists == false"
    description: role does not exist
  ready:
    when: "exists == true"
    description: role defined

default_active_state: ready

failure_modes:
  missing:
    can_cause: [permission_denied]
```

#### clusterrole

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get clusterrole {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  rule_count:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl get clusterrole {name} -o json"
      parse: "json:.rules | length"
      cost: low

healthy:
  - exists == true

states:
  missing:
    when: "exists == false"
    description: cluster role does not exist
  ready:
    when: "exists == true"
    description: cluster role defined

default_active_state: ready

failure_modes:
  missing:
    can_cause: [permission_denied]
```

#### rolebinding

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get rolebinding {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  subject_count:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get rolebinding {name} -o json"
      parse: "json:.subjects | length"
      cost: low
  role_ref_exists:
    type: mgtt.bool
    ttl: 60s
    description: whether the referenced role actually exists
    probe:
      cmd: "kubectl -n {namespace} get role $(kubectl -n {namespace} get rolebinding {name} -o jsonpath={.roleRef.name}) -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: medium

healthy:
  - exists == true
  - role_ref_exists == true

states:
  missing:
    when: "exists == false"
    description: role binding does not exist
  dangling:
    when: "exists == true & role_ref_exists == false"
    description: references a role that does not exist
  ready:
    when: "exists == true & role_ref_exists == true"
    description: binding active

default_active_state: ready

failure_modes:
  missing:
    can_cause: [permission_denied]
  dangling:
    can_cause: [permission_denied]
```

#### clusterrolebinding

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get clusterrolebinding {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  subject_count:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl get clusterrolebinding {name} -o json"
      parse: "json:.subjects | length"
      cost: low
  role_ref_exists:
    type: mgtt.bool
    ttl: 60s
    description: whether the referenced cluster role actually exists
    probe:
      cmd: "kubectl get clusterrole $(kubectl get clusterrolebinding {name} -o jsonpath={.roleRef.name}) -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: medium

healthy:
  - exists == true
  - role_ref_exists == true

states:
  missing:
    when: "exists == false"
    description: cluster role binding does not exist
  dangling:
    when: "exists == true & role_ref_exists == false"
    description: references a cluster role that does not exist
  ready:
    when: "exists == true & role_ref_exists == true"
    description: binding active

default_active_state: ready

failure_modes:
  missing:
    can_cause: [permission_denied]
  dangling:
    can_cause: [permission_denied]
```

### Category: Webhooks

#### validatingwebhookconfiguration

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get validatingwebhookconfiguration {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  webhook_count:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl get validatingwebhookconfiguration {name} -o json"
      parse: "json:.webhooks | length"
      cost: low
  service_available:
    type: mgtt.bool
    ttl: 30s
    description: whether the webhook backend service has ready endpoints
    probe:
      cmd: "kubectl get validatingwebhookconfiguration {name} -o json"
      parse: "json:.webhooks[0].clientConfig.service"
      cost: medium
  failure_policy:
    type: mgtt.string
    ttl: 60s
    probe:
      cmd: "kubectl get validatingwebhookconfiguration {name} -o jsonpath={.webhooks[0].failurePolicy}"
      parse: string
      cost: low

healthy:
  - exists == true
  - service_available == true

states:
  missing:
    when: "exists == false"
    description: webhook configuration does not exist
  backend_down:
    when: "exists == true & service_available == false & failure_policy == \"Fail\""
    description: webhook backend unreachable — blocking cluster operations
  backend_degraded:
    when: "exists == true & service_available == false & failure_policy == \"Ignore\""
    description: webhook backend unreachable — silently skipped
  active:
    when: "exists == true & service_available == true"
    description: webhook active and backend reachable

default_active_state: active

failure_modes:
  backend_down:
    can_cause: [deployment_blocked, upstream_failure]
  backend_degraded:
    can_cause: [security_violation]
```

#### mutatingwebhookconfiguration

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get mutatingwebhookconfiguration {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  webhook_count:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl get mutatingwebhookconfiguration {name} -o json"
      parse: "json:.webhooks | length"
      cost: low
  service_available:
    type: mgtt.bool
    ttl: 30s
    description: whether the webhook backend service has ready endpoints
    probe:
      cmd: "kubectl get mutatingwebhookconfiguration {name} -o json"
      parse: "json:.webhooks[0].clientConfig.service"
      cost: medium
  failure_policy:
    type: mgtt.string
    ttl: 60s
    probe:
      cmd: "kubectl get mutatingwebhookconfiguration {name} -o jsonpath={.webhooks[0].failurePolicy}"
      parse: string
      cost: low

healthy:
  - exists == true
  - service_available == true

states:
  missing:
    when: "exists == false"
    description: webhook configuration does not exist
  backend_down:
    when: "exists == true & service_available == false & failure_policy == \"Fail\""
    description: webhook backend unreachable — blocking cluster mutations
  backend_degraded:
    when: "exists == true & service_available == false & failure_policy == \"Ignore\""
    description: webhook backend unreachable — mutations silently unmodified
  active:
    when: "exists == true & service_available == true"
    description: webhook active and backend reachable

default_active_state: active

failure_modes:
  backend_down:
    can_cause: [deployment_blocked, upstream_failure]
  backend_degraded:
    can_cause: [security_violation, data_inconsistency]
```

### Category: API / Scheduling / Extensibility

#### customresourcedefinition

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get crd {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  established:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get crd {name} -o jsonpath={.status.conditions[?(@.type=='Established')].status}"
      parse: bool
      cost: low
  names_accepted:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get crd {name} -o jsonpath={.status.conditions[?(@.type=='NamesAccepted')].status}"
      parse: bool
      cost: low

healthy:
  - established == true
  - names_accepted == true

states:
  missing:
    when: "exists == false"
    description: CRD not registered
  not_established:
    when: "exists == true & established == false"
    description: CRD not yet ready to accept instances
  name_conflict:
    when: "names_accepted == false"
    description: CRD name conflicts with another resource
  ready:
    when: "established == true & names_accepted == true"
    description: CRD established and accepting instances

default_active_state: ready

failure_modes:
  missing:
    can_cause: [upstream_failure, deployment_blocked]
  not_established:
    can_cause: [upstream_failure, deployment_blocked]
  name_conflict:
    can_cause: [upstream_failure, deployment_blocked]
```

#### priorityclass

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl get priorityclass {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  value:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl get priorityclass {name} -o jsonpath={.value}"
      parse: int
      cost: low
  preemption_policy:
    type: mgtt.string
    ttl: 60s
    probe:
      cmd: "kubectl get priorityclass {name} -o jsonpath={.preemptionPolicy}"
      parse: string
      cost: low

healthy:
  - exists == true

states:
  missing:
    when: "exists == false"
    description: priority class not defined
  ready:
    when: "exists == true"
    description: priority class available

default_active_state: ready

failure_modes:
  missing:
    can_cause: [resource_contention, upstream_failure]
```

#### lease

```yaml
facts:
  exists:
    type: mgtt.bool
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get lease {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  holder:
    type: mgtt.string
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get lease {name} -o jsonpath={.spec.holderIdentity}"
      parse: string
      cost: low
  renew_age:
    type: mgtt.int
    ttl: 30s
    description: seconds since last renewal
    probe:
      cmd: "kubectl -n {namespace} get lease {name} -o jsonpath={.spec.renewTime}"
      parse: age_seconds
      cost: low
  lease_duration:
    type: mgtt.int
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get lease {name} -o jsonpath={.spec.leaseDurationSeconds}"
      parse: int
      cost: low

healthy:
  - exists == true
  - renew_age < lease_duration

states:
  missing:
    when: "exists == false"
    description: lease does not exist
  expired:
    when: "renew_age > lease_duration"
    description: holder has not renewed — leader election stale
  held:
    when: "exists == true & holder != \"\""
    description: lease actively held

default_active_state: held

failure_modes:
  missing:
    can_cause: [upstream_failure]
  expired:
    can_cause: [upstream_failure, data_inconsistency]
```

#### custom_resource

```yaml
# Variables: api_version, kind, name, namespace
# Generic type for any CRD instance (ArgoCD Application, ExternalSecret, Certificate, etc.)
facts:
  exists:
    type: mgtt.bool
    ttl: 60s
    probe:
      cmd: "kubectl -n {namespace} get {kind}.{api_version} {name} -o jsonpath={.metadata.name}"
      parse: "regex:.+"
      cost: low
  ready:
    type: mgtt.bool
    ttl: 30s
    description: status condition Ready=True (common convention)
    probe:
      cmd: "kubectl -n {namespace} get {kind}.{api_version} {name} -o jsonpath={.status.conditions[?(@.type=='Ready')].status}"
      parse: bool
      cost: low
  synced:
    type: mgtt.bool
    ttl: 30s
    description: status condition Synced=True (common in ArgoCD, Crossplane)
    probe:
      cmd: "kubectl -n {namespace} get {kind}.{api_version} {name} -o jsonpath={.status.conditions[?(@.type=='Synced')].status}"
      parse: bool
      cost: low
  age:
    type: mgtt.int
    ttl: 60s
    description: seconds since creation
    probe:
      cmd: "kubectl -n {namespace} get {kind}.{api_version} {name} -o jsonpath={.metadata.creationTimestamp}"
      parse: age_seconds
      cost: low

healthy:
  - exists == true
  - ready == true

states:
  missing:
    when: "exists == false"
    description: custom resource does not exist
  not_ready:
    when: "exists == true & ready == false"
    description: resource exists but not ready
  not_synced:
    when: "exists == true & synced == false"
    description: resource not synced with desired state
  ready:
    when: "exists == true & ready == true"
    description: resource ready

default_active_state: ready

failure_modes:
  missing:
    can_cause: [upstream_failure, deployment_blocked]
  not_ready:
    can_cause: [upstream_failure, timeout]
  not_synced:
    can_cause: [upstream_failure, data_inconsistency]
```

## New Parse Modes

The type definitions introduce one new parse mode not present in the current provider:

- **`age_seconds`** — takes an ISO 8601 timestamp string and returns the number of seconds elapsed since that time. Used by `cronjob.last_schedule_age`, `lease.renew_age`, `secret.age`, `configmap.age`, `volumeattachment.age`, `custom_resource.age`.

This requires a small addition to the probe executor's parse logic.

## New Failure Effects

The type definitions introduce failure effects beyond the original set. Full vocabulary:

| Effect | Description |
|--------|-------------|
| `upstream_failure` | downstream component cannot reach this one |
| `timeout` | requests to this component time out |
| `connection_refused` | connection actively rejected |
| `5xx_errors` | HTTP 500-class errors |
| `dns_failure` | DNS resolution fails |
| `data_loss` | data may be permanently lost |
| `data_inconsistency` | data may be stale or inconsistent |
| `stale_data` | data not being refreshed |
| `resource_contention` | insufficient resources for scheduling |
| `deployment_blocked` | new deployments/changes cannot proceed |
| `node_drain_stuck` | node drain cannot complete |
| `permission_denied` | RBAC prevents access |
| `security_violation` | security policy bypassed or missing |
| `scheduled_task_skipped` | cron or scheduled work not executing |

## Example: Modeling Magento-Planform

With this provider, the magento-planform setup could be modeled as:

```yaml
meta:
  name: magento-staging
  version: 1.0.0
  providers: [kubernetes, aws]
  vars:
    namespace: default

components:
  # Prerequisites
  magento-namespace:
    type: namespace
    vars: { name: default }

  magento-sa:
    type: serviceaccount
    vars: { name: magento }
    depends:
      - on: magento-namespace

  ecr-registry-secret:
    type: secret
    vars: { name: ecr-registry }
    depends:
      - on: magento-namespace

  nginx-config-blue:
    type: configmap
    vars: { name: magento-nginx-config-blue }

  env-php-blue:
    type: secret
    vars: { name: magento-env-php-blue }

  # Ingress layer
  magento-ingress:
    type: ingress
    vars: { name: magento-ingress }
    depends:
      - on: magento-svc

  # Service layer
  magento-svc:
    type: service
    vars: { name: magento-svc }
    depends:
      - on: nginx-blue

  # Workloads
  nginx-blue:
    type: deployment
    vars: { name: magento-nginx-blue }
    depends:
      - on: magento-sa
      - on: ecr-registry-secret
      - on: nginx-config-blue
      - on: php-fpm-blue

  php-fpm-blue:
    type: deployment
    vars: { name: magento-php-fpm-blue }
    depends:
      - on: magento-sa
      - on: ecr-registry-secret
      - on: env-php-blue
      # AWS-provider components (future):
      # - on: rds-mysql
      # - on: elasticache-redis
      # - on: amazonmq

  magento-cron:
    type: deployment
    vars: { name: magento-cron }
    depends:
      - on: magento-sa
      - on: ecr-registry-secret

  # Scaling
  nginx-hpa:
    type: hpa
    vars: { name: magento-nginx-blue-hpa }
    depends:
      - on: nginx-blue

  php-fpm-hpa:
    type: hpa
    vars: { name: magento-php-fpm-blue-hpa }
    depends:
      - on: php-fpm-blue

  # Availability
  nginx-pdb:
    type: pdb
    vars: { name: magento-nginx-pdb }
    depends:
      - on: nginx-blue

  php-fpm-pdb:
    type: pdb
    vars: { name: magento-php-fpm-pdb }
    depends:
      - on: php-fpm-blue
```

## Implementation Scope

### Orthogonal change: multi-file provider loading

**Files affected:**
- `internal/providersupport/load.go` — detect missing `types:` key, scan `types/` directory, merge into provider
- `internal/providersupport/load_test.go` — test both single-file and multi-file loading

### Provider YAML files

- `providers/kubernetes/provider.yaml` — meta, auth, variables (no types)
- `providers/kubernetes/types/*.yaml` — 37 individual type files

### Existing testdata

- `internal/providersupport/testdata/kubernetes.yaml` — keep as-is for backward-compatibility testing, or migrate to multi-file format alongside

### Parse mode addition

- `age_seconds` parse mode in the probe executor

### No engine changes

The engine, expression evaluator, simulator, and model loader are unchanged. The provider is pure vocabulary — the engine already knows how to consume any types/facts/states/failure_modes the provider defines.
