# Binary Protocol

Your provider binary implements a simple protocol: mgtt calls it with args, it returns JSON on stdout.

## On this page

- [Commands](#commands) — argv, stdout, exit codes
- [Example: Go binary](#example-go-binary)
- [Example: Bash binary](#example-bash-binary)
- [Next steps](#next-steps)

---

## Commands

### `probe` — collect a fact

The primary operation. mgtt calls this when it needs a fact value from a live system.

```bash
mgtt-provider-my-provider probe <component> <fact> \
  --namespace <ns> --type <type>
```

Return JSON on stdout:

```json
{"value": 42, "raw": "42"}
```

| Field | Description |
|-------|-------------|
| `value` | The typed parsed value (int, float, bool, or string) |
| `raw` | The raw output string (for audit/display) |

Exit `0` on success. Exit non-zero on failure with error message on stderr.

### `validate` — check auth and connectivity

```bash
mgtt-provider-my-provider validate --namespace <ns>
```

Return:

```json
{"ok": true, "auth": "config at ~/.my-tool/config", "access": "read-only"}
```

### `describe` — self-declare capabilities

Optional. Supplements `manifest.yaml`.

```bash
mgtt-provider-my-provider describe
```

---

## Example: Go binary

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
        fmt.Println(`{"ok": true, "auth": "config loaded", "access": "read-only"}`)

    default:
        fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
        os.Exit(1)
    }
}

func probe(component, fact, namespace, componentType string) (*Result, error) {
    switch fact {
    case "connected":
        ok := true // replace with your actual connectivity check
        return &Result{Value: ok, Raw: fmt.Sprintf("%v", ok)}, nil

    case "response_time":
        ms := 42.5 // replace with your actual latency measurement
        return &Result{Value: ms, Raw: fmt.Sprintf("%.1f", ms)}, nil

    default:
        return nil, fmt.Errorf("unknown fact: %s", fact)
    }
}
```

## Example: Bash binary

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

Any language works — Go, Python, Bash, Rust. The only contract is: accept args, write JSON to stdout.

---

## Next steps

- [Vocabulary](vocabulary.md) — writing manifest.yaml
- [Writing install scripts](overview.md#writing-install-scripts) — build scripts for Go, Python, pre-compiled
- [Testing](testing.md) — validate, simulate, and live-test your provider
