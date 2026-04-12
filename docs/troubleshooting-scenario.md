# Troubleshooting with mgtt

This walkthrough shows mgtt in action against a real incident. Two phases: setup (done once, calmly) and incident response (done at 3am, pressing Y).

## The System

A storefront running on EKS — nginx fronting a React frontend and a Node.js API, backed by an AWS RDS database.

```mermaid
graph LR
  internet([internet]) --> nginx
  nginx[nginx\nreverse proxy] --> frontend
  nginx --> api
  frontend[frontend\nReact SPA] --> api
  api[api\nNode.js] --> rds[(rds\nAWS RDS)]

  style nginx     fill:#E1F5EE,stroke:#0F6E56,color:#085041
  style frontend  fill:#E1F5EE,stroke:#0F6E56,color:#085041
  style api       fill:#E1F5EE,stroke:#0F6E56,color:#085041
  style rds       fill:#E6F1FB,stroke:#185FA5,color:#0C447C
  style internet  fill:#F1EFE8,stroke:#5F5E5A,color:#444441
```

---

## Setup (done once)

*Done by whoever knows the system. Not during an incident.*

### 1. Install providers

mgtt uses credentials already in your environment — kubectl config, AWS profile, etc.

```bash
$ mgtt provider install kubernetes aws

  ✓ kubernetes  v1.0.0  auth: environment  access: kubectl read-only
  ✓ aws         v0.1.0  auth: environment  access: AWS API read-only
```

### 2. Write the model

```bash
$ mgtt init

  ✓ created system.model.yaml
```

Edit it to describe your system:

```yaml
# system.model.yaml
meta:
  name: storefront
  version: "1.0"
  providers:
    - kubernetes
  vars:
    namespace: production

components:
  nginx:
    type: ingress
    depends:
      - on: frontend
      - on: api

  frontend:
    type: deployment
    depends:
      - on: api

  api:
    type: deployment
    depends:
      - on: rds

  rds:
    providers:
      - aws
    type: rds_instance
    healthy:
      - connection_count < 500
```

### 3. Validate

```bash
$ mgtt model validate

  ✓ nginx     2 dependencies valid
  ✓ frontend  1 dependency valid
  ✓ api       1 dependency valid
  ✓ rds       healthy override valid

  4 components · 0 errors · 0 warnings
```

Commit alongside your Helm charts and Terraform. Setup is done.

---

## The Incident

*Monday 08:14 UTC. Alert fires: "503 errors on checkout."*

### 4. Start the incident

```bash
$ mgtt incident start

  ✓ inc-20240205-0814-001 started
```

### 5. Run the guided plan

```bash
$ mgtt plan

  starting from outermost component: nginx

  -> probe nginx upstream_count
     cost: low | kubectl read-only

  run? [Y/n] y

  ✓ nginx.upstream_count = 0   ✗ unhealthy

  3 paths to investigate:
  PATH A   nginx <- frontend
  PATH B   nginx <- api
  PATH C   nginx <- api <- rds

  -> probe api endpoints
     cost: low | eliminates PATH B, PATH C if healthy

  run? [Y/n] y

  ✓ api.endpoints = 0   ✗ unhealthy

  -> probe api ready_replicas
     cost: low | kubectl read-only

  run? [Y/n] y

  ✓ api.ready_replicas = 0   ✗ unhealthy

  -> probe api restart_count
     cost: low

  run? [Y/n] y

  ✓ api.restart_count = 47   ✗ unhealthy

  -> probe rds available
     cost: low | AWS API read-only | eliminates PATH C if healthy

  run? [Y/n] y

  ✓ rds.available = true   ✓ healthy

  -> probe frontend ready_replicas
     cost: low | kubectl read-only | eliminates PATH A if healthy

  run? [Y/n] y

  ✓ frontend.ready_replicas = 2   ✓ healthy

  Root cause: api
  Path:       nginx <- api
  State:      degraded
  Eliminated: frontend, rds
```

The engine probed 4 components in 6 steps. It eliminated rds (healthy) and frontend (healthy), and traced the fault to api — crash-looping with 47 restarts and 0 of 3 replicas ready.

### 6. Check logs, record findings

```bash
$ kubectl logs deploy/api -n production --previous | tail -3
Error: Cannot find module './config/feature-flags'

$ mgtt fact add api startup_error "missing module: ./config/feature-flags" \
      --note "kubectl logs --previous"

$ mgtt fact add api last_deploy_at "2024-02-05T07:50:00Z" \
      --note "deploy 24min before incident"
```

### 7. Close the incident

```bash
$ mgtt incident end

  inc-20240205-0814-001   duration: 14 minutes

  ✗ api       crash-looping
              startup_error: missing module ./config/feature-flags
              last_deploy:   07:50Z (24min before incident)
  ✓ rds       healthy · eliminated
  ✓ frontend  healthy · eliminated

  probes: 6 · facts: 8

  ✓ closed · state file: ./inc-20240205-0814-001.state.yaml
```

The state file is the incident record — timestamped, structured, complete. No separate postmortem write-up needed for the facts.

---

## Summary

What the on-call engineer did:

```
mgtt incident start
mgtt plan
y · y · y · y · y · y
mgtt fact add (x2, manual observations)
mgtt incident end
```

**14 minutes. 6 probes. Root cause identified. No system knowledge required at incident time.**

All the system knowledge was encoded in the model beforehand. The engineer just pressed Y.

---

## Alternative entry points

The example above starts from the outermost component (nginx) and works inward. Two alternatives when you already have information:

```bash
# Start from a known-bad component
mgtt plan --component api

# Pre-load a fact from an alert, then plan
mgtt fact add api error_rate 0.94 --note "datadog alert"
mgtt plan --component api
```

---

## Before the incident

The model and failure scenarios can be validated before the system is deployed. See [Simulation Scenario](./simulation-scenario.md) for the design-time workflow — writing scenarios, running them in CI, and what failing scenarios reveal about the model.

The same `system.model.yaml` serves both phases. The scenarios written at design time are the tests that prevent model gaps from becoming incident blind spots.
