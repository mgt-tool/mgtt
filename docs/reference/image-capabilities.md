# Provider Capabilities

Providers declare **what they need from the environment** at probe time. Git-installed providers get those needs satisfied for free (the binary inherits the operator's shell). Image-installed providers depend on mgtt forwarding bind mounts and env vars via `docker run` flags. This page is the reference for the capability vocabulary that drives the latter.

## Declaring capabilities

In your `provider.yaml`, at the top level:

```yaml
needs: [kubectl, aws]
network: host
```

Both fields are optional.

- **`needs:`** — named packages/credential chains/sockets the provider wants access to. HTTP-only providers that configure their URL via `vars:` can omit it entirely.
- **`network:`** — docker-run network mode, a separate runtime isolation setting (see the [`network` section](#network-mode) below).

Top-level because a provider's environmental requirements are a property of the provider itself, not of any one install method. The image runner is the subsystem that today translates `needs` into `docker run` flags; if new install methods land, the same vocabulary applies — no schema change.

## Built-in vocabulary

Every entry in `needs:` names a host-side package or credential chain. Labels expand to bind mounts and env forwards — never to runtime-mode flags (those live on `network:`, below).

| Capability | What mgtt injects |
|---|---|
| `kubectl` | `-v $HOME/.kube:/root/.kube:ro`; `-e KUBECONFIG=…` (when set) |
| `aws` | `-v $HOME/.aws:/root/.aws:ro`; `-e` for each of `AWS_PROFILE`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_REGION`, `AWS_DEFAULT_REGION` (when set) |
| `docker` | `-v /var/run/docker.sock:/var/run/docker.sock` |
| `terraform` | `-v $PWD:/workspace -w /workspace`; `-e TF_CLI_CONFIG_FILE` and every `TF_VAR_*` set in the caller |
| `gcloud` | `-v $HOME/.config/gcloud:/root/.config/gcloud:ro`; `-e GOOGLE_APPLICATION_CREDENTIALS` and every `CLOUDSDK_*` set |
| `azure` | `-v $HOME/.azure:/root/.azure:ro`; `-e` for `ARM_CLIENT_ID`, `ARM_CLIENT_SECRET`, `ARM_TENANT_ID`, `ARM_SUBSCRIPTION_ID` (when set) |

Env passthrough emits `-e KEY=VALUE` only when `KEY` is set in the caller. Unset keys are silently skipped so Docker never consumes the next positional arg.

## Network mode

`network:` is a separate top-level field because it names an isolation mode, not a host resource. Docker's three modes are supported:

| Value | Effect |
|---|---|
| `bridge` (default) | Container gets a NAT'd virtual NIC. Reaches the internet; **cannot** reach the host's localhost, private interfaces, or in-cluster DNS (`*.svc`). |
| `host` | Container shares the host's network namespace — sees all interfaces, localhost services, host-configured DNS. Required for kubectl against a private cluster, Terraform against a VPC'd state backend, anything resolving `*.svc` or `host.docker.internal`. |
| `none` | No network. Mostly a security posture for probes that touch only local state. |

Omitting `network:` defaults to `bridge`. Non-default values land in the docker-run argv as an explicit `--network <mode>` flag; operators see the mode in the `mgtt provider install --image` output and can audit it before use.

## Operator overrides

Drop a file at `$MGTT_HOME/capabilities.yaml`, or shards under `$MGTT_HOME/capabilities.d/*.yaml`:

```yaml
capabilities:
  # Override built-in kubectl for a non-default kubeconfig
  kubectl:
    - "-v"
    - "/etc/kubernetes/admin.conf:/root/.kube/config:ro"
    - "-e"
    - "KUBECONFIG=/root/.kube/config"

  # Define a custom capability used by an internal provider
  tibco:
    - "-v"
    - "/etc/tibco/cert.pem:/root/cert.pem:ro"
    - "-e"
    - "TIBCO_BROKER_URL"
```

Precedence (highest wins): `MGTT_IMAGE_CAP_<NAME>` env var → operator file → built-in.

**Env-var one-liner** (for CI without a file):

```
MGTT_IMAGE_CAP_KUBECTL="-v /etc/k.conf:/root/.kube/config:ro -e KUBECONFIG=/root/.kube/config"
```

Argv is shell-split; single and double quotes group tokens.

## Opt-out

```
MGTT_IMAGE_CAPS_DENY=docker,aws
```

Comma-separated list. mgtt refuses to inject these capabilities regardless of provider declaration. The probe still runs — without the forwards — and likely fails with an honest error, not silent wrong state.

## Validation

`mgtt provider validate` and `mgtt provider install --image` both check `needs` against the merged vocabulary (built-ins ∪ operator file ∪ env overrides) and check `network:` against the fixed set `{bridge, host, none}`. Unknown caps fail with a message naming the offending label and the known-names list. An invalid `network:` value fails naming the bad mode. Shell-fallback providers (no `meta.command`) cannot declare capabilities — there's no binary to run, so no install method has anything to inject them into.
