# mgtt provider SDK

Build a mgtt provider runner binary in Go with ~20 lines of code.

```go
package main

import (
    "context"

    "github.com/mgt-tool/mgtt/sdk/provider"
    "github.com/mgt-tool/mgtt/sdk/provider/shell"
)

func main() {
    kubectl := shell.New("kubectl")
    kubectl.Classify = classifyKubectl // your stderr-pattern map; see below

    r := provider.NewRegistry()
    r.Register("deployment", map[string]provider.ProbeFn{
        "ready_replicas": func(ctx context.Context, req provider.Request) (provider.Result, error) {
            data, err := kubectl.RunJSON(ctx, "-n", req.Namespace, "get", "deploy", req.Name, "-o", "json")
            if err != nil {
                return provider.Result{}, err
            }
            // ... extract status.readyReplicas
            return provider.IntResult(3), nil
        },
    })
    provider.Main(r)
}
```

## What the SDK gives you

| Feature | Provided by |
|---|---|
| Argv parsing (`probe <name> <fact> --type T --key value …`) | `provider.Main` / `provider.Run` |
| `version` subcommand | `provider.Main` (set `provider.Version` via ldflags) |
| Exit codes per probe protocol | `provider.Main` |
| `status: not_found` translation when probe returns `provider.ErrNotFound` | `provider.Registry.Probe` |
| Backend-CLI invocation with timeout, size cap, env-not-found classification | `shell.Client` |
| Custom backend stderr → sentinel error mapping | `shell.Client.Classify` (your function) |

## Building

```bash
go build -ldflags "-X github.com/mgt-tool/mgtt/sdk/provider.Version=$(cat VERSION)" -o bin/<runner> .
```

## Backend-specific classification

The SDK is **backend-agnostic** — it does not know about kubectl, aws, docker, etc. The default classifier (`shell.EnvOnlyClassify`) only handles "binary not on PATH". For every other error class, supply your own:

```go
func classifyKubectl(stderr string, runErr error) error {
    if runErr == nil {
        return nil
    }
    switch {
    case strings.Contains(stderr, "NotFound"):
        return provider.ErrNotFound
    case strings.Contains(stderr, "Forbidden"):
        return provider.ErrForbidden
    case strings.Contains(stderr, "Unable to connect"):
        return provider.ErrTransient
    }
    return shell.EnvOnlyClassify(stderr, runErr) // fallback
}
```

This keeps backend vocabulary out of mgtt core.

## Read-only contract

Declare your provider's access surface in `manifest.yaml`:

```yaml
auth:
  access:
    probes: <human-readable description of what the provider reads>
    writes: none           # REQUIRED; mgtt provider validate fails otherwise
```

`writes: none` is a contract you make with operators. mgtt cannot enforce it directly — operators are responsible for binding credentials that match (kubernetes RBAC, cloud IAM, daemon-socket permissions, scoped tokens, POSIX permissions, or "no credentials needed").

Providers SHOULD ship a `CREDENTIALS.md` next to `manifest.yaml` describing how to provision least-privilege credentials for the backend. The format is provider-specific because authorization models vary completely:

- A kubernetes provider may include ClusterRole YAML.
- An AWS provider may include an IAM policy JSON.
- A docker provider may describe how to mount a read-only socket.
- A provider against an authz-free public API may simply state "no credentials required".

## Contract

The full wire protocol between mgtt and your runner lives at [`docs/PROBE_PROTOCOL.md`](../../docs/PROBE_PROTOCOL.md) in the mgtt repo. The SDK implements it for you; consult the protocol doc only when you need to write a non-Go provider or debug the wire format.
