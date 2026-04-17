# Provider Capabilities

Reference for the `needs:` and `network:` fields in `manifest.yaml` and how mgtt expands them at probe time.

## Declaring

```yaml
needs: [kubectl, aws]
network: host
```

Both fields are optional. Providers that don't talk to host resources omit `needs:` entirely.

## `needs` vocabulary

| Capability | Expansion |
|---|---|
| `kubectl` | `-v $HOME/.kube:/root/.kube:ro`; `-e KUBECONFIG=…` (when set) |
| `aws` | `-v $HOME/.aws:/root/.aws:ro`; `-e` for each of `AWS_PROFILE`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_REGION`, `AWS_DEFAULT_REGION` (when set) |
| `docker` | `-v /var/run/docker.sock:/var/run/docker.sock` |
| `terraform` | `-v $PWD:/workspace -w /workspace`; `-e TF_CLI_CONFIG_FILE` and every `TF_VAR_*` set in the caller |
| `gcloud` | `-v $HOME/.config/gcloud:/root/.config/gcloud:ro`; `-e GOOGLE_APPLICATION_CREDENTIALS` and every `CLOUDSDK_*` set |
| `azure` | `-v $HOME/.azure:/root/.azure:ro`; `-e` for `ARM_CLIENT_ID`, `ARM_CLIENT_SECRET`, `ARM_TENANT_ID`, `ARM_SUBSCRIPTION_ID` (when set) |

Env forwards emit `-e KEY=VALUE` only when `KEY` is set in the caller.

## `network` values

| Value | Effect |
|---|---|
| `bridge` (default) | NAT'd virtual NIC. Reaches the internet; cannot reach host localhost, private interfaces, or in-cluster DNS. |
| `host` | Container shares the host's network namespace. Required for in-cluster DNS (`*.svc`), private API endpoints, `host.docker.internal`. |
| `none` | No network. |

Omitting `network:` is equivalent to `bridge`. Non-default values add `--network <mode>` to the docker-run line.

## Operator overrides

File at `$MGTT_HOME/capabilities.yaml` (drop-in shards at `$MGTT_HOME/capabilities.d/*.yaml` are also loaded):

```yaml
capabilities:
  kubectl:
    - "-v"
    - "/etc/kubernetes/admin.conf:/root/.kube/config:ro"
    - "-e"
    - "KUBECONFIG=/root/.kube/config"

  tibco:
    - "-v"
    - "/etc/tibco/cert.pem:/root/cert.pem:ro"
    - "-e"
    - "TIBCO_BROKER_URL"
```

Env-var one-liner:

```
MGTT_IMAGE_CAP_KUBECTL="-v /etc/k.conf:/root/.kube/config:ro -e KUBECONFIG=/root/.kube/config"
```

Argv is shell-split; quotes group tokens. Precedence: env var > operator file > built-in.

## Opt-out

```
MGTT_IMAGE_CAPS_DENY=docker,aws
```

Comma-separated list of capabilities mgtt refuses to inject. Applies at probe time regardless of provider declaration.

## Validation

`mgtt provider validate` and `mgtt provider install --image`:

- Each entry in `needs:` must resolve against the merged vocabulary (built-ins ∪ operator file ∪ env).
- `network:` must be one of `bridge`, `host`, `none`, or omitted.
- Providers with no `meta.command` cannot declare `needs:`.
