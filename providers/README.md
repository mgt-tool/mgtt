# Writing an `mgtt` Provider

A provider teaches mgtt about a technology. You provide three things:

1. **The vocabulary** (`provider.yaml`) — types, facts, states, failure modes, written in mgtt's language
2. **The binary** — a program that probes real systems and returns typed values
3. **The install hook** — a script that builds or downloads the binary

That's it. mgtt handles the reasoning; your provider handles the observing.

## Directory Structure

```
my-provider/
├── provider.yaml              vocabulary for the constraint engine
├── hooks/
│   └── install.sh             builds the binary (runs during mgtt provider install)
├── go.mod                     (if writing in Go — any language works)
├── main.go                    implements the binary protocol
└── bin/
    └── mgtt-provider-my-provider   compiled binary (gitignored)
```

## Multi-File Provider Structure

For providers with many types (like the Kubernetes provider with 37 types), you can
split type definitions into individual files:

```
my-provider/
├── provider.yaml              meta, auth, variables, hooks (no types: key)
├── types/
│   ├── deployment.yaml        one file per type
│   ├── service.yaml
│   └── ...
├── hooks/
│   └── install.sh
└── bin/
    └── mgtt-provider-my-provider
```

Each `.yaml` file in `types/` contains exactly what would go under `types.<name>:` in
a single-file provider. The filename (minus `.yaml`) becomes the type name.

**Backward-compatible**: providers with inline `types:` in `provider.yaml` still work.
The loader checks for `types:` first; if absent, it scans the `types/` directory.

Load multi-file providers with `LoadFromDir("path/to/my-provider")`.

## Step 1: Write the Vocabulary (`provider.yaml`)

The vocabulary tells mgtt's constraint engine what your technology looks like —
what component types exist, what facts can be observed, what states are possible,
and how failures propagate. You fill in mgtt's schema with your technology's specifics.

```yaml
meta:
  name: my-provider
  version: 0.1.0
  description: One-line description of what this provider covers
  requires:
    mgtt: ">=1.0"
  command: "$MGTT_PROVIDER_DIR/bin/mgtt-provider-my-provider"

hooks:
  install: hooks/install.sh

# read_only defaults to true — omit when your provider is a pure reader.
# Set to false and describe the side effect in writes_note when it isn't.

variables:
  namespace:
    description: target namespace
    required: false
    default: default

types:
  server:
    description: A server instance

    facts:
      connected:
        type: mgtt.bool
        ttl: 15s
        cost: low
        access: network read

      response_time:
        type: mgtt.float
        ttl: 30s
        cost: low

    healthy:
      - connected == true
      - response_time < 500

    states:
      live:
        when: "connected == true & response_time < 500"
        description: responding normally
      degraded:
        when: "connected == true & response_time >= 500"
        description: slow responses
      stopped:
        when: "connected == false"
        description: not responding

    default_active_state: live

    failure_modes:
      degraded:
        can_cause: [timeout, upstream_failure]
      stopped:
        can_cause: [upstream_failure, connection_refused]
```

### Vocabulary Reference

**`meta`** — provider identity and binary location.
- `name`: lowercase, hyphen-separated, unique across the ecosystem
- `version`: semver
- `command`: path to the provider binary. `$MGTT_PROVIDER_DIR` is substituted at runtime with the provider's install directory
- `hooks.install`: script to run during `mgtt provider install`

**`types`** — the component types your technology has. Each type declares:
- `facts`: observable properties. Each fact has a `type` (from mgtt's stdlib: `mgtt.int`, `mgtt.float`, `mgtt.bool`, `mgtt.string`, etc.), a `ttl` (staleness threshold), and a `cost` (low/medium/high)
- `healthy`: conditions that must ALL hold for the component to be healthy. Uses mgtt's expression syntax: `fact_name <op> value`, joined with `&` (and) or `|` (or)
- `states`: ordered list of possible states. Evaluated top-to-bottom — **first match wins**. Put specific states before general ones (e.g., `degraded` before `starting`)
- `default_active_state`: the "normal" state. Components in this state are considered healthy by the engine
- `failure_modes`: for each non-healthy state, what downstream effects it can cause. Values from the standard vocabulary: `upstream_failure`, `connection_refused`, `timeout`, `5xx_errors`, `query_timeout`, `dns_failure`, `auth_failure`, `resource_exhaustion`

**`facts.probe`** — optional inline probe definition for providers without a binary
(the shell-fallback path). When a provider has a binary (`meta.command`), the binary
handles probing and the `probe` block is metadata only. When no binary exists, mgtt
executes `probe.cmd` as a shell command.

```yaml
facts:
  ready_replicas:
    type: mgtt.int
    ttl: 30s
    probe:
      cmd: "kubectl -n {namespace} get deploy {name} -o jsonpath={.status.readyReplicas}"
      parse: int       # int | float | bool | string | exit_code | json:<path> | lines:<N> | regex:<pat>
      cost: low        # low | medium | high
      access: kubectl read-only
```

A provider can be **vocabulary-only** (no binary, no install hook) if all facts
have inline `probe.cmd` definitions. This is the quick-start path — useful for
prototyping before investing in a compiled binary.

**`variables`** — parameters the model author can set in `meta.vars`. Substituted into probe commands as `{variable_name}`.

**`read_only` / `writes_note`** — the provider's write posture. `read_only: true` (default) means pure reads. Set to `false` and describe the side effect in `writes_note` when the provider touches anything — state files, webhooks, credential stores. `mgtt provider install` prints the note so operators consent knowingly. Credentials themselves (which env vars, which config files) belong in the provider's README, not here — they're narrative, not structured.

### State Ordering Matters

States are evaluated top-to-bottom. First match wins. This means:

```yaml
# CORRECT — specific before general
states:
  degraded:
    when: "ready_replicas < desired_replicas & restart_count > 5"
  starting:
    when: "ready_replicas < desired_replicas"
```

```yaml
# WRONG — starting matches first, degraded is unreachable
states:
  starting:
    when: "ready_replicas < desired_replicas"
  degraded:
    when: "ready_replicas < desired_replicas & restart_count > 5"
```

`mgtt provider validate` catches this.

### Available Stdlib Types

Use `mgtt stdlib ls` to see all primitive types:

```
int         base: int      unit: ~
float       base: float    unit: ~
bool        base: bool     unit: ~
string      base: string   unit: ~
duration    base: float    unit: ms|s|m|h|d
bytes       base: int      unit: b|kb|mb|gb|tb
ratio       base: float    range: 0..1
percentage  base: float    range: 0..100
count       base: int      range: 0..
timestamp   base: string   unit: ISO8601
```

Reference them as `mgtt.int`, `mgtt.bool`, etc. in your fact type declarations.

## Step 2: Write the Binary

Your binary implements a simple protocol: mgtt calls it with args, it returns JSON on stdout.

> **For Go providers, use the SDK.** The [`sdk/provider`](../sdk/provider/README.md) package
> implements the protocol for you — argv parsing, version subcommand, exit-code mapping,
> `status: not_found` translation, and a generic backend-CLI helper with timeout, size cap,
> and pluggable error classification. A complete provider is ~20 lines:
>
> ```go
> import "github.com/mgt-tool/mgtt/sdk/provider"
>
> func main() {
>     r := provider.NewRegistry()
>     r.Register("server", map[string]provider.ProbeFn{
>         "connected": probeConnected,
>     })
>     provider.Main(r)
> }
> ```
>
> The wire-protocol details below are still authoritative — see [`docs/PROBE_PROTOCOL.md`](../docs/PROBE_PROTOCOL.md) — but you only need to read them if you're writing a provider in another language or debugging the wire format.

### The Protocol

**`probe`** — the primary operation. mgtt calls this when it needs a fact value:

```bash
mgtt-provider-my-provider probe <component> <fact> \
  --namespace <ns> --type <type>
```

Return JSON on stdout:
```json
{"value": 42, "raw": "42"}
```

- `value`: the typed parsed value (int, float, bool, or string)
- `raw`: the raw output string (for audit/display)

Exit 0 on success. Exit non-zero on failure with error message on stderr.

**`validate`** — check that auth and connectivity work:

```bash
mgtt-provider-my-provider validate --namespace <ns>
```

Return:
```json
{"ok": true, "auth": "config at ~/.my-tool/config", "access": "read-only"}
```

**`describe`** — self-declare capabilities (optional, supplements provider.yaml):

```bash
mgtt-provider-my-provider describe
```

### Example Binary (Go)

```go
package main

import (
    "encoding/json"
    "fmt"
    "os"
)

type Result struct {
    Value any    `json:"value"`
    Raw   string `json:"raw"`
}

func main() {
    if len(os.Args) < 4 {
        fmt.Fprintf(os.Stderr, "usage: mgtt-provider-my-provider probe <component> <fact> [flags]\n")
        os.Exit(1)
    }

    command := os.Args[1]
    component := os.Args[2]
    fact := os.Args[3]

    // Parse flags
    namespace := "default"
    componentType := ""
    for i := 4; i < len(os.Args)-1; i++ {
        switch os.Args[i] {
        case "--namespace":
            namespace = os.Args[i+1]
        case "--type":
            componentType = os.Args[i+1]
        }
    }

    switch command {
    case "probe":
        result, err := probe(component, fact, namespace, componentType)
        if err != nil {
            fmt.Fprintf(os.Stderr, "probe error: %v\n", err)
            os.Exit(1)
        }
        json.NewEncoder(os.Stdout).Encode(result)

    case "validate":
        // Check connectivity to your system
        fmt.Println(`{"ok": true, "auth": "config loaded", "access": "read-only"}`)

    default:
        fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
        os.Exit(1)
    }
}

func probe(component, fact, namespace, componentType string) (*Result, error) {
    switch fact {
    case "connected":
        // Replace with your actual connectivity check
        ok := true // e.g. ping, TCP connect, API health endpoint
        return &Result{Value: ok, Raw: fmt.Sprintf("%v", ok)}, nil

    case "response_time":
        // Replace with your actual latency measurement
        ms := 42.5 // e.g. time an HTTP request
        return &Result{Value: ms, Raw: fmt.Sprintf("%.1f", ms)}, nil

    default:
        return nil, fmt.Errorf("unknown fact: %s", fact)
    }
}
```

You can write the binary in any language. Python, Bash, Rust — anything that
accepts args and writes JSON to stdout.

### Bash Example

```bash
#!/bin/bash
# mgtt-provider-simple — a provider in 20 lines

component="$2"
fact="$3"

case "$fact" in
  connected)
    if ping -c1 -W1 "$component" &>/dev/null; then
      echo '{"value": true, "raw": "true"}'
    else
      echo '{"value": false, "raw": "false"}'
    fi
    ;;
  *)
    echo "unknown fact: $fact" >&2
    exit 1
    ;;
esac
```

## Step 3: Write the Install Hook

`hooks/install.sh` runs during `mgtt provider install`. It produces the binary.

### For Go providers:

```bash
#!/bin/bash
set -e
cd "$(dirname "$0")/.."
mkdir -p bin
go build -o bin/mgtt-provider-my-provider .
echo "✓ built bin/mgtt-provider-my-provider"
```

### For pre-compiled binaries:

```bash
#!/bin/bash
set -e
cd "$(dirname "$0")/.."
mkdir -p bin

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in x86_64) ARCH="amd64" ;; aarch64) ARCH="arm64" ;; esac

VERSION=$(grep 'version:' provider.yaml | head -1 | awk '{print $2}')
URL="https://github.com/my-org/mgtt-provider-my-provider/releases/download/v${VERSION}/mgtt-provider-my-provider-${OS}-${ARCH}"

curl -sSL "$URL" -o bin/mgtt-provider-my-provider
chmod +x bin/mgtt-provider-my-provider
echo "✓ downloaded mgtt-provider-my-provider v${VERSION}"
```

### For Python providers:

```bash
#!/bin/bash
set -e
cd "$(dirname "$0")/.."
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt
mkdir -p bin
cat > bin/mgtt-provider-my-provider <<'WRAPPER'
#!/bin/bash
exec "$(dirname "$0")/../.venv/bin/python" "$(dirname "$0")/../main.py" "$@"
WRAPPER
chmod +x bin/mgtt-provider-my-provider
echo "✓ installed Python provider"
```

## Step 4: Publish a Docker Image (optional but recommended)

Providers can be installed two ways:

- **Git install** (`mgtt provider install <url>`) — clones the repo and runs `hooks/install.sh` to build the binary on the host. Requires the Go (or Python, or whatever) toolchain locally.
- **Image install** (`mgtt provider install --image <ref>@sha256:...`) — pulls a Docker image that ships the compiled binary and the `provider.yaml`. mgtt invokes probes via `docker run` against the image. The host only needs `docker`.

Image install is the recommended distribution path for corporate operators — no local toolchain, digest-pinned reproducibility, and works identically across machines.

### Image Contract

The image must satisfy three requirements:

1. **`/provider.yaml` at the root** — the vocabulary, extracted by mgtt at install time via `docker create` + `docker cp`. Any base image works, including distroless and scratch — mgtt never executes anything inside the container during extraction.
2. **`ENTRYPOINT` is the provider binary** — mgtt invokes probes with `docker run --rm <image> probe <component> <fact> ...`. The entrypoint must accept the same argv the host-installed binary does.
3. **Image is digest-pinned when published to the registry** — tags can be re-rolled silently. Always publish (and install) with `@sha256:...`.

### Minimal Dockerfile

```Dockerfile
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/mgtt-provider-my-provider .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/mgtt-provider-my-provider /bin/provider
COPY provider.yaml /provider.yaml
COPY types /types
ENTRYPOINT ["/bin/provider"]
```

The `COPY types /types` line is only needed for multi-file providers (those that keep one type per file under `types/<name>.yaml`). mgtt's image installer `docker cp`s both `/provider.yaml` and (if present) `/types/` out of the container.

Publish to `ghcr.io` (or any registry) and advertise the digest in your README and the registry entry.

### Declaring capabilities

Providers that need host context at probe time declare **capabilities** at the top level of `provider.yaml`:

```yaml
needs: [kubectl, aws]
network: host
```

Top-level because the requirement ("this provider needs kubectl and cluster network") is a property of the provider itself. Git installs satisfy needs by inheriting the operator's shell; image installs satisfy them via `docker run` bind mounts and env forwards that mgtt wires from the capability vocabulary. The built-in set covers `kubectl`, `aws`, `docker`, `terraform`, `gcloud`, `azure`; operators override built-ins or define custom labels via `$MGTT_HOME/capabilities.yaml`. See [Provider Capabilities](../docs/reference/image-capabilities.md) for the full reference.

`network:` is a separate field (not a capability label) because it names a runtime isolation mode, not a host resource. Valid values: `bridge` (default), `host`, `none`. Anything that needs in-cluster DNS (`*.svc`) or host-local services declares `network: host`; external-HTTPS-only providers can omit it.

Shell-fallback providers (no `meta.command`) must omit both `needs:` and `network:` — there's no binary to attach the forwards to.

### Registry Entry with Image

Your provider's registry entry can declare both install methods:

```yaml
my-provider:
  url: https://github.com/my-org/mgtt-provider-my-provider
  image: ghcr.io/my-org/mgtt-provider-my-provider:0.1.0@sha256:...
  description: ...
```

Users pick: `mgtt provider install my-provider` (git) or `mgtt provider install --image ghcr.io/my-org/mgtt-provider-my-provider:0.1.0@sha256:...` (image).

### Install Hooks Don't Run for Image Installs

Image installs skip `hooks/install.sh` entirely — the binary is already baked in. Image uninstalls also skip `hooks/uninstall.sh`; instead `mgtt provider uninstall` prints a `docker rmi` hint so the operator can clean up the image if they want.

If your provider has a meaningful uninstall side effect (e.g., revoking credentials), document it for image users — mgtt can't run arbitrary scripts inside the image at uninstall time.

## Uninstall Hook (optional)

`hooks/uninstall.sh` runs during `mgtt provider uninstall <name>`, before the
provider directory is removed. Use it to clean up build artifacts, revoke
credentials, deregister webhooks, etc. If the hook fails, the directory is
still removed — uninstall must always succeed.

```bash
#!/bin/bash
set -e
cd "$(dirname "$0")/.."
echo "cleaning up..."
rm -rf bin/ .venv/
echo "✓ cleanup complete"
```

Declare it in `provider.yaml`:

```yaml
hooks:
  install: hooks/install.sh
  uninstall: hooks/uninstall.sh
```

Uninstall does NOT check `meta.requires.mgtt` — you must always be able to
remove a provider you can no longer use.

## Installing Your Provider

From a local directory:
```bash
mgtt provider install ./path/to/my-provider
```

From a git repository:
```bash
mgtt provider install https://github.com/my-org/mgtt-provider-my-provider
```

From a Docker image (digest-pinned):
```bash
mgtt provider install --image ghcr.io/my-org/mgtt-provider-my-provider:0.1.0@sha256:...
```

After install:
```bash
mgtt provider ls                          # verify it shows up (shows install method: git | image)
mgtt provider inspect my-provider         # check types and facts
mgtt provider inspect my-provider server  # detailed type view
```

Each install writes a `.mgtt-install.json` into `~/.mgtt/providers/<name>/`:

```json
{
  "method": "image",
  "namespace": "my-org",
  "source": "ghcr.io/my-org/mgtt-provider-my-provider:0.1.0@sha256:...",
  "installed_at": "2026-04-17T10:30:00Z",
  "version": "0.1.0"
}
```

mgtt uses this metadata to drive `provider list`, fully-qualified name (FQN) references, and SemVer constraint resolution. See [Provider FQN & Versions](../docs/concepts/provider-fqn-and-versions.md) and [Install Methods](../docs/concepts/provider-install-methods.md).

## Testing Your Provider

### 1. Validate the vocabulary:

```bash
mgtt provider validate ./my-provider
```

Checks: YAML syntax, state ordering, fact types resolve against stdlib,
failure_modes reference declared states, expressions parse correctly.

### 2. Test the binary directly:

```bash
./bin/mgtt-provider-my-provider validate --namespace production
./bin/mgtt-provider-my-provider probe myserver connected --namespace production --type server
```

### 3. Write simulation scenarios:

Create scenarios that inject facts from your provider and assert the engine
reasons correctly:

```yaml
# scenarios/server-down.yaml
name: server unreachable
inject:
  myserver:
    connected: false
expect:
  root_cause: myserver
```

```bash
mgtt simulate --scenario scenarios/server-down.yaml
```

### 4. Test against a real system:

```bash
mgtt provider install ./my-provider
mgtt plan --component myserver
```

## Reference Implementations

Providers live in their own repositories, not under this directory. Study these for a complete working example:

- [mgtt-provider-kubernetes](https://github.com/mgt-tool/mgtt-provider-kubernetes) — 37-type vocabulary (multi-file `types/`), Go binary using kubectl
- [mgtt-provider-docker](https://github.com/sajonaro/mgtt-provider-docker) — Docker provider

Each repo shows `provider.yaml` vocabulary, `main.go` runner, and `hooks/install.sh`.

## Design Principles

- **Vocabulary is mgtt's language.** You fill in the blanks with your technology's knowledge. The schema is the same for every provider.
- **The binary is a black box.** mgtt doesn't care how you probe — kubectl, API call, SSH, curl. Args in, JSON out.
- **The engine never calls your binary for reasoning.** It reads `provider.yaml` to build failure paths. The binary is only called when it's time to actually observe a component.
- **Any language works.** Go is convenient (single binary, fast), but Bash, Python, Rust, or anything that speaks the protocol is fine.
- **State ordering is your responsibility.** The engine evaluates states top-to-bottom, first match wins. Put specific conditions before general ones.
