# manifest.yaml reference

Every mgtt provider ships a `manifest.yaml` at the root of its repository. It declares identity, runtime requirements, and install methods. Three top-level blocks.

## `meta:` — identity

```yaml
meta:
  name:         aws                                # required; [a-z][a-z0-9-]*
  version:      1.0.0                              # required; semver
  description:  AWS resources for mgtt             # required
  tags:         [cloud, aws]                       # optional
  requires:
    mgtt:       ">=0.2.0"                          # semver range
```

## `runtime:` — how the provider talks to its backend

```yaml
runtime:
  needs: [aws]                                     # list shorthand: no version constraints
  # — OR enriched:
  needs:
    aws: ">=2.13"                                  # version constraint on backing tool

  backends: [quickwit]                             # list shorthand
  # — OR:
  backends:
    quickwit: ">=0.8 <0.10"                        # backend-service compat

  network_mode: host                               # bridge (default) | host
  entrypoint:   "$MGTT_PROVIDER_DIR/bin/…"         # optional; convention-default
```

`needs:` keys resolve against the capability vocabulary (see [Provider Capabilities](image-capabilities.md)). `network_mode` is a suggestion — operators can override per install or via `MGTT_NETWORK_MODE`.

## `install:` — how the provider comes to exist on a machine

```yaml
install:
  source:                                          # offers source install
    build: hooks/install.sh                        # script that produces the binary
    clean: hooks/uninstall.sh                      # script that undoes it
  image:                                           # offers image install
    repository: ghcr.io/mgt-tool/mgtt-provider-aws # optional; defaults from registry
```

At least one of `install.source` or `install.image` must be declared. `mgtt provider install <name>` picks source when available; `mgtt provider install --image <ref>` forces image. Methods not declared are rejected up-front.

## Invariants

- `meta.name` matches `^[a-z][a-z0-9-]*$`.
- `meta.version` and all constraint strings parse as semver.
- `network_mode` is `bridge` or `host` when present.
- `install` must declare at least one method.
- `needs:` / `backends:` are either list or map, never mixed within a single declaration.

## Defaults

- `runtime.network_mode` — `bridge` when omitted.
- `runtime.entrypoint` (source install) — `$MGTT_PROVIDER_DIR/bin/mgtt-provider-<name>`.
- `runtime.entrypoint` (image install) — the image's baked-in `ENTRYPOINT`.
- `install.image.repository` — derived from the registry URL as `ghcr.io/<owner>/mgtt-provider-<name>` when the registry entry points at a github.com repo.
