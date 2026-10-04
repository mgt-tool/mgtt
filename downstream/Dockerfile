# The downstream regression image: this checkout of mgtt, uncommitted changes
# included, built and tested, then every repository that depends on it built
# against it and run.
#
#   make downstream                         # from the mgtt checkout
#   docker run --rm mgtt-downstream         # rerun every suite, no rebuild
#   docker run --rm mgtt-downstream mcp     # rerun one suite
#   docker run --rm mgtt-downstream list    # the suites
#
# A successful build is a pass: each suite is a RUN in its own check-* stage.
# A suite listed in downstream/xfail is a known breakage tied to a step of the
# improvement plan. It passes while it fails, and fails the moment it passes,
# so the list cannot go stale.
#
# Everything is pinned. Images by digest, toolchains by exact version, every
# repository by full commit SHA. *_REF may name a branch or tag, but
# downstream/build.sh resolves it to a SHA before building, so the clone cache
# is keyed on the commit and a rebuild is reproducible.
#
# Works under the classic builder too, so no RUN --mount, ADD <git url> or
# --build-context.

ARG GO_IMAGE=golang:1.25.7-bookworm@sha256:564e366a28ad1d70f460a2b97d1d299a562f08707eb0ecb24b659e5bd6c108e1
ARG OCAML_IMAGE=ocaml/opam:debian-12-ocaml-5.2@sha256:b06c7348b6ed3e83b5f66267254504a8f066ec9be9c020c30a8e28ce10ab3ef6
# writ 0.4.0.
ARG WRIT_IMAGE=ghcr.io/writ-lang/writ@sha256:a5999fd8b626ac035491863eeb5cb9224efe8fb81c553226b5bb44923151255c
# Debian 12, matching GO_IMAGE's and OCAML_IMAGE's glibc: provider binaries are
# built with cgo on and copied across.
ARG SUITES_IMAGE=debian:bookworm-slim@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251

# ---- writ: the published runtime image, for its binaries ---------------------
FROM ${WRIT_IMAGE} AS writ

# ---- engine: build and test this checkout ------------------------------------
FROM ${GO_IMAGE} AS engine
# The pinned toolchain only: never let a go.mod `toolchain` line download
# another one.
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src/mgtt
# Before the COPY, so a source edit does not redownload modules.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The same targets a developer types.
RUN make test build \
 && install -m 755 mgtt /usr/local/bin/mgtt \
 && mgtt version

# ---- provider-*: each provider built and tested against this engine ----------
# FROM engine: the provider's go.mod is pointed at /src/mgtt, so it compiles
# against the SDK and engine under test, not the release it pins. The build and
# install must succeed. The unit-test outcome is recorded instead, and the
# provider-* suites assert on it, so a known test breakage can sit in xfail
# without hiding the providers that pass.
FROM engine AS providers
ENV CGO_ENABLED=1 MGTT_HOME=/mgtt-home
COPY downstream/provider.sh /usr/local/bin/provider.sh

# One RUN per repository, ARGs just above it, so a new commit in one reclones
# only that one.
ARG AWS_REPO=https://github.com/mgt-tool/mgtt-provider-aws.git
ARG AWS_REF=ac81ae462f2ae11ac06219d24d227d964c19aa5f
RUN provider.sh aws "$AWS_REPO" "$AWS_REF"

ARG DOCKER_REPO=https://github.com/mgt-tool/mgtt-provider-docker.git
ARG DOCKER_REF=500dad9041b284cb631a3223c0961a639b92e248
RUN provider.sh docker "$DOCKER_REPO" "$DOCKER_REF"

ARG KUBERNETES_REPO=https://github.com/mgt-tool/mgtt-provider-kubernetes.git
ARG KUBERNETES_REF=b30b2ede1914de9e577893d8bdf7b9bfe132c82c
RUN provider.sh kubernetes "$KUBERNETES_REPO" "$KUBERNETES_REF"

ARG QUICKWIT_REPO=https://github.com/mgt-tool/mgtt-provider-quickwit.git
ARG QUICKWIT_REF=3158e45de9f76a1701e268e6be0002665350d7db
RUN provider.sh quickwit "$QUICKWIT_REPO" "$QUICKWIT_REF"

ARG TEMPO_REPO=https://github.com/mgt-tool/mgtt-provider-tempo.git
ARG TEMPO_REF=48bd1c0e76829c3269346b59dc76b9ed4003c2e5
RUN provider.sh tempo "$TEMPO_REPO" "$TEMPO_REF"

ARG TERRAFORM_REPO=https://github.com/mgt-tool/mgtt-provider-terraform.git
ARG TERRAFORM_REF=7f9d66f6a163ab1205918686bf34c009c4ee1e32
RUN provider.sh terraform "$TERRAFORM_REPO" "$TERRAFORM_REF"

# ---- mgtt2writ: build and unit-test the translator ---------------------------
FROM ${OCAML_IMAGE} AS mgtt2writ
# The opam repository inside the pinned image is a fixed snapshot; no
# `opam update`, so this resolves to the same dune every time.
RUN opam install -y dune.3.24.2
ARG MGTT2WRIT_REPO=https://github.com/mgt-tool/mgtt2writ.git
ARG MGTT2WRIT_REF=b606fa7964f2d763a6705e01433f926b56ae6071
WORKDIR /home/opam
RUN git clone -q "$MGTT2WRIT_REPO" mgtt2writ \
 && cd mgtt2writ && git checkout -q "$MGTT2WRIT_REF" \
 && git log --oneline -1 \
 && opam exec -- dune build \
 && opam exec -- dune runtest --force \
 && cp -L _build/default/bin/main.exe /home/opam/mgtt2writ.bin

# ---- suites: the binaries, the providers, the examples -----------------------
FROM ${SUITES_IMAGE} AS suites

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates=20250419~deb12u1 \
      git=1:2.39.5-0+deb12u3 \
 && rm -rf /var/lib/apt/lists/*

# Non-root, as the downstream images run.
RUN useradd -m runner && mkdir /downstream /mgtt-home && chown runner:runner /downstream /mgtt-home
ENV MGTT_HOME=/mgtt-home DOWNSTREAM=/downstream

COPY --from=writ /usr/local/bin/writ /usr/local/bin/writ-cert /usr/local/bin/
COPY --from=writ /usr/local/share/writ/lib /usr/local/share/writ/lib

# Fixture and binary from the same mgtt2writ commit.
COPY --from=mgtt2writ --chown=runner:runner /home/opam/mgtt2writ/test /downstream/mgtt2writ/test
COPY --from=mgtt2writ /home/opam/mgtt2writ.bin /usr/local/bin/mgtt2writ

COPY --from=providers --chown=runner:runner /mgtt-home /mgtt-home

# mgtt last: it changes on every edit, and everything above it does not.
COPY --from=engine /usr/local/bin/mgtt /usr/local/bin/mgtt
COPY --chown=runner:runner examples /downstream/examples
COPY --chown=runner:runner downstream/run.sh downstream/xfail /downstream/

USER runner
WORKDIR /downstream

# ---- check-*: one stage per suite --------------------------------------------
FROM suites AS check-providers
RUN /downstream/run.sh providers && touch /tmp/passed

FROM suites AS check-provider-aws
RUN /downstream/run.sh provider-aws && touch /tmp/passed

FROM suites AS check-provider-docker
RUN /downstream/run.sh provider-docker && touch /tmp/passed

FROM suites AS check-provider-kubernetes
RUN /downstream/run.sh provider-kubernetes && touch /tmp/passed

FROM suites AS check-provider-quickwit
RUN /downstream/run.sh provider-quickwit && touch /tmp/passed

FROM suites AS check-provider-tempo
RUN /downstream/run.sh provider-tempo && touch /tmp/passed

FROM suites AS check-provider-terraform
RUN /downstream/run.sh provider-terraform && touch /tmp/passed

FROM suites AS check-minishop
RUN /downstream/run.sh minishop && touch /tmp/passed

FROM suites AS check-storefront
RUN /downstream/run.sh storefront && touch /tmp/passed

FROM suites AS check-storefront-speed
RUN /downstream/run.sh storefront-speed && touch /tmp/passed

FROM suites AS check-redundant-web
RUN /downstream/run.sh redundant-web && touch /tmp/passed

FROM suites AS check-mgtt2writ
RUN /downstream/run.sh mgtt2writ && touch /tmp/passed

FROM suites AS check-mcp
RUN /downstream/run.sh mcp && touch /tmp/passed

FROM suites AS check-mcp-probe
RUN /downstream/run.sh mcp-probe && touch /tmp/passed

# ---- all: the default target -------------------------------------------------
# BuildKit skips stages nothing depends on, so a check not COPYed here would
# silently stop running.
FROM suites AS all
COPY --from=check-providers            /tmp/passed /tmp/passed-providers
COPY --from=check-provider-aws         /tmp/passed /tmp/passed-provider-aws
COPY --from=check-provider-docker      /tmp/passed /tmp/passed-provider-docker
COPY --from=check-provider-kubernetes  /tmp/passed /tmp/passed-provider-kubernetes
COPY --from=check-provider-quickwit    /tmp/passed /tmp/passed-provider-quickwit
COPY --from=check-provider-tempo       /tmp/passed /tmp/passed-provider-tempo
COPY --from=check-provider-terraform   /tmp/passed /tmp/passed-provider-terraform
COPY --from=check-minishop             /tmp/passed /tmp/passed-minishop
COPY --from=check-storefront           /tmp/passed /tmp/passed-storefront
COPY --from=check-storefront-speed     /tmp/passed /tmp/passed-storefront-speed
COPY --from=check-redundant-web        /tmp/passed /tmp/passed-redundant-web
COPY --from=check-mgtt2writ            /tmp/passed /tmp/passed-mgtt2writ
COPY --from=check-mcp                  /tmp/passed /tmp/passed-mcp
COPY --from=check-mcp-probe            /tmp/passed /tmp/passed-mcp-probe

ENTRYPOINT ["/downstream/run.sh"]
CMD ["all"]
