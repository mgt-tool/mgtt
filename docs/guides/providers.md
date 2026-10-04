# Providers

A provider teaches mgtt one technology: its component types, their facts and states, and how to probe them. Browse the [registry](../reference/registry.md).

## Install and pin

```bash
mgtt provider install kubernetes aws                  # by registry name
mgtt provider install mgt-tool/kubernetes@>=3.0.0      # with a version constraint
mgtt provider install https://github.com/you/mgtt-provider-redis
mgtt provider install --image ghcr.io/you/mgtt-provider-redis:1.0.0@sha256:…
mgtt provider ls | inspect <name> [type] | uninstall <name>
```

A source install builds the provider locally (Go is usually needed). An image install needs only Docker. Each probe then runs in a fresh container.

In the model, reference providers by full name and version constraint. When several installed versions match, the highest wins.

```yaml
meta:
  providers:
    - mgt-tool/kubernetes@>=3.0.0,<4.0.0   # also: exact 3.0.0, caret ^3.0
```

A bare name (`kubernetes`) still works, but it warns. If a reference matches nothing installed, mgtt prints the install command that would fix it.

## Credentials

Providers use the credentials already in your environment: kubeconfig, AWS profile, Docker socket. Grant them read-only. A provider declares `read_only: true` and `diagnose` only runs those by default, but only your RBAC/IAM can actually enforce that.

Image providers declare which host access they need (`runtime.needs: [kubectl, aws]`). mgtt then mounts and forwards exactly that access:

| Need | Mounted / forwarded |
|---|---|
| `kubectl` | `~/.kube` (ro), `KUBECONFIG` |
| `aws` | `~/.aws` (ro), `AWS_PROFILE`, `AWS_REGION`, access-key vars |
| `docker` | `/var/run/docker.sock` |
| `terraform` | `$PWD` as `/workspace`, `TF_VAR_*` |
| `gcloud`, `azure` | their config dir and standard env vars |

To override a capability, use `$MGTT_HOME/capabilities.yaml` or `MGTT_IMAGE_CAP_KUBECTL="-v /etc/k.conf:/root/.kube/config:ro"`. To refuse one, use `MGTT_IMAGE_CAPS_DENY=docker,aws`. A provider that needs in-cluster DNS declares `network_mode: host`.

## Generating a model

`mgtt model build` asks every installed provider that supports discovery to list what it manages, then writes `system.model.yaml`. If a rebuild would drop a component, it refuses unless you pass `--allow-deletes` or `--tombstone <name>`. That protects against an expired token quietly emptying your model. Edits you make by hand are overwritten on rebuild, so keep the generated file and your edits separate. None of the official providers implements discovery yet.
