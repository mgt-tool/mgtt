# `depends:` — how failure spreads

`depends: [{on: X}]` means "if X fails, I may fail". The engine traces symptoms back along these edges to a root cause, and clears a branch when a component on it is proven healthy.

## Plain dependencies

```yaml
api:
  depends:
    - on: db
    - on: cache
```

Each one is hard: a broken `db` can break `api`.

## Redundancy groups: active/active

When either of several components can carry the load *right now*, use a group:

```yaml
svc:
  depends:
    - on: [web-a, web-b]
      need: 1          # served while at least one is healthy
```

One member down does not break `svc`. That member is reported as "redundancy degraded", not as the root cause. A failure under every member (a shared database) still breaks the group and is the root cause. A member whose health could not be read does not count toward `need`.

## Guards: active/passive

A blue/green pair behind a Service whose selector names the live color is **not** a group. Traffic does not move to the idle color, so a dead live color is an outage. Follow the selector with `while:`:

```yaml
shop-svc:
  type: service
  vars:
    selector_key: color            # the selector label to read
  depends:
    - on: web-blue
      while: selector_value == blue
    - on: web-green
      while: selector_value == green
```

An edge whose `while:` is false is not followed. Until `selector_value` is probed the guard is undecided and both edges are followed, which is the cautious choice.

Rule of thumb: either serves now, use `need:`. One serves and you switch, use `while:`.
