# Provider Names and Versions

Models should be explicit about which provider (and which version) they expect. FQN + version constraint eliminates "it worked on my machine" provider drift.

## On this page

- [Show me](#show-me) — four reference forms, side by side
- [Resolution algorithm](#resolution-algorithm) — how mgtt picks the right install
- [Version constraint syntax](#version-constraint-syntax) — supported forms and what they mean
- [Bare-name deprecation](#bare-name-deprecation) — legacy form still works, with a warning

---

## Show me

```yaml
providers:
  - mgt-tool/kubernetes@>=0.5.0,<1.0.0   # FQN + range — recommended
  - mgt-tool/tempo@0.2.0                 # FQN + exact pin
  - mgt-tool/aws@^0.2                    # FQN + caret (compatible range)
  - kubernetes                            # legacy bare name — works, warns
```

When `mgtt plan` runs, the resolver matches each ref against locally-installed providers. Unresolved refs produce a clear error with the install command to fix it:

```
provider resolution failed; 1 unresolved ref(s):
  no installed provider satisfies "mgt-tool/kubernetes@>=0.5.0,<1.0.0" (constraint: ">=0.5.0,<1.0.0"); install with: mgtt provider install mgt-tool/kubernetes@>=0.5.0,<1.0.0
```

---

## Resolution algorithm

1. **For FQN refs:** match on namespace + name + version constraint. If multiple installed versions satisfy the constraint, the highest version wins.
2. **For bare names:** match on name only (any namespace). A warning is emitted on stderr suggesting the FQN form.
3. **Unresolved refs:** the error lists ALL missing providers, each with the exact install command that would fix it — so the operator can resolve everything in one pass.
4. **Install captures namespace automatically:** when you install from a git URL or image, mgtt derives the namespace from the source. Operators never set `namespace` by hand.

---

## Version constraint syntax

| Constraint | Meaning | Example |
|---|---|---|
| `0.2.0` | exact | only `0.2.0` |
| `>=0.5.0` | at least | `0.5.0`, `0.6.0`, `1.0.0` |
| `<1.0.0` | below | `0.9.9` yes, `1.0.0` no |
| `>=0.5.0,<1.0.0` | range (AND) | both must hold |
| `^0.2` | caret | `>=0.2.0,<0.3.0` |

Comma joins constraints with AND — both must hold. The caret form follows the convention: `^0.x` locks to the minor (`>=0.x.0,<0.(x+1).0`), and `^1.x` locks to the major (`>=1.x.0,<2.0.0`).

No constraint at all matches any installed version.

---

## Bare-name deprecation

Bare names still work. You'll see a warning at plan/simulate/validate time suggesting the FQN form. There's no timeline to remove bare-name support, but new models should always use FQN — it removes ambiguity when multiple providers share a short name across different namespaces.

---

## See also

- [Provider Install Methods](./provider-install-methods.md) — git vs image
- [Multi-File Models](./multi-file-models.md) — when one system needs multiple model files
