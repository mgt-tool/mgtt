# Writing Providers

A provider teaches mgtt about a technology. You provide three things:

1. **The vocabulary** (`provider.yaml`) — types, facts, states, failure modes
2. **The binary** — probes real systems, returns typed values
3. **The install hook** — builds or downloads the binary

## Directory Structure

```
my-provider/
├── provider.yaml              vocabulary for the engine
├── hooks/
│   └── install.sh             builds the binary
├── main.go                    implements the protocol
└── bin/
    └── mgtt-provider-my-provider
```

## Install

```bash
# From a local directory
mgtt provider install ./my-provider

# From a git repository
mgtt provider install https://github.com/user/mgtt-provider-redis
```

## Next Steps

- [Vocabulary](vocabulary.md) — writing provider.yaml
- [Binary Protocol](protocol.md) — implementing probe/validate/describe
- [Install Hooks](hooks.md) — Go, Python, and pre-compiled examples
- [Testing](testing.md) — validate, simulate, and live-test your provider
