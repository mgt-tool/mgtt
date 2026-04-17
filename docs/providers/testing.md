# Testing Providers

Four levels of testing, from static checks to live probes.

## On this page

1. [Validate the vocabulary](#1-validate-the-vocabulary)
2. [Test the binary directly](#2-test-the-binary-directly)
3. [Write simulation scenarios](#3-write-simulation-scenarios)
4. [Test against a real system](#4-test-against-a-real-system)
- [Reference implementation](#reference-implementation)
- [Next steps](#next-steps)

---

## 1. Validate the vocabulary

```bash
mgtt provider validate ./my-provider
```

Checks: YAML syntax, state ordering, fact types resolve against stdlib, failure_modes reference declared states, expressions parse correctly.

## 2. Test the binary directly

```bash
./bin/mgtt-provider-my-provider validate --namespace production
./bin/mgtt-provider-my-provider probe myserver connected --namespace production --type server
```

Expected output:

```json
{"ok": true, "auth": "config loaded", "access": "read-only"}
{"value": true, "raw": "true"}
```

## 3. Write simulation scenarios

Create scenarios that inject facts from your provider's types and assert the engine reasons correctly:

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

This tests the vocabulary — are the states, health conditions, and failure modes wired correctly?

## 4. Test against a real system

```bash
mgtt provider install ./my-provider
mgtt plan --component myserver
```

This tests the binary — does it actually collect facts from a live system?

---

## Reference implementation

The [kubernetes provider](https://github.com/mgt-tool/mgtt-provider-kubernetes) is the reference implementation:

- `manifest.yaml` — full vocabulary with 2 types, 5 facts, 4 states
- `main.go` — binary using kubectl JSON output
- `hooks/install.sh` — Go build script

The [aws provider](https://github.com/mgt-tool/mgtt-provider-aws) shows a minimal vocabulary-only provider (no binary).

---

## Next steps

- [Vocabulary](vocabulary.md) — writing manifest.yaml
- [Binary Protocol](protocol.md) — implementing probe/validate/describe
- [Install Hooks](hooks.md) — build scripts
