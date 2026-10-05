# Writing a provider

A provider is a directory with a `manifest.yaml`, type definitions, and usually a runner binary.

```
mgtt-provider-redis/
├── manifest.yaml
├── types/cluster.yaml
├── hooks/install.sh        # builds bin/mgtt-provider-redis (source installs)
└── main.go
```

## Manifest

```yaml
meta:
  name: redis
  version: 1.0.0
  description: Redis clusters via redis-cli
  requires:
    mgtt: ">={{ MGTT_VERSION }}"          # only the >= form is accepted
runtime:
  needs: [kubectl]           # host access for image installs
install:                     # at least one of source / image
  source:
    build: hooks/install.sh
    clean: hooks/uninstall.sh
  image:
    repository: ghcr.io/you/mgtt-provider-redis
read_only: true              # default; set false and add writes_note if probes write
variables:
  max_clients: { description: client limit, default: 1000 }   # models override via vars:
```

## Types

```yaml
# types/cluster.yaml
facts:
  connected_clients: { type: mgtt.int, ttl: 30s, cost: low }
  role:              { type: mgtt.string, ttl: 60s }
healthy:
  - connected_clients < max_clients
states:
  saturated: { when: "connected_clients >= max_clients" }
  live:      { when: "connected_clients < max_clients" }
default_active_state: live
failure_modes:
  saturated:
    can_cause: [timeout, connection_refused]
```

`failure_modes` labels are what connect a failure to the components that depend on it. A fact with `probe: {cmd, parse}` runs a shell command. A provider that defines every fact this way needs no binary at all.

A fact can be derived over a trailing window, so health rests on a trend rather than a level: `queue_depth_delta_5m: { type: mgtt.int, window: 5m, derive: delta }`. `derive` is `delta` (last sample minus first), `rate` (that per second) or `max`. The provider computes the value, and the engine and scenarios see an ordinary number.

## The runner protocol

mgtt invokes `bin/mgtt-provider-<name> probe <component> <fact> --type <type> [--window 5m --derive delta] [--<var> <value> …]`, with every model var passed as a flag; `--window` and `--derive` come with a derived fact. On success, the runner writes one JSON line to stdout:

```json
{"value": 42, "raw": "42 clients", "status": "ok"}
```

`status: "not_found"` (with `value: null`) means the resource doesn't exist. Failures go to stderr, with these exit codes:

| Exit | Meaning |
|---|---|
| 1 | usage: bad args, unknown type or fact |
| 2 | environment: kubectl/aws CLI missing |
| 3 | forbidden: credentials refused |
| 4 | transient: timeout, throttling, 5xx |
| 5 | protocol: backend returned garbage |

Exit codes 3 and 4 become `Cannot rule out` in a diagnosis, never "healthy". Each probe has a 30-second timeout and a 10 MiB output limit. When `MGTT_DEBUG=1` is set, debug output goes to stderr only.

Optional subcommands: `version`, and `discover`, which prints `{"components":[{"name","type"}],"dependencies":[{"from","to"}]}` for `mgtt model build`.

## The Go SDK

```go
import "github.com/mgt-tool/mgtt/sdk/provider"

func main() {
    r := provider.NewRegistry()
    r.Register("cluster", map[string]provider.ProbeFn{
        "connected_clients": func(ctx context.Context, req provider.Request) (provider.Result, error) {
            return provider.IntResult(42), nil
        },
    })
    provider.Main(r)   // handles argv, JSON, exit codes, version, discover
}
```

For a derived fact, register `provider.Windowed(series)`: `series` returns the samples taken since the window began, and the SDK reduces them as the request's `--window` and `--derive` say. One series serves every window a type declares over it. Too few samples to tell is transient: the fact is unknown, not zero.

The SDK is Apache-2.0, so your provider is not bound by the engine's AGPL. `sdk/provider/shell` wraps CLI calls and classifies their errors.

## Test and publish

```bash
mgtt provider install ./mgtt-provider-redis
mgtt provider validate redis          # static checks: manifest, states, capabilities
mgtt provider inspect redis cluster
```

Test probes against a real backend in the provider's own CI. Write a model scenario for each state, so `mgtt simulate` covers your types. To list the provider, add it to [`docs/registry.yaml`](https://github.com/mgt-tool/mgtt/blob/main/docs/registry.yaml) with a pull request.
