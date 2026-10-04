#!/bin/sh
# Copyright (C) 2026 Alex Kunich
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Build the downstream regression image. Every repository is pinned to a full
# commit SHA: by default the one in downstream/Dockerfile, or a branch or tag
# given in the environment, which is resolved to its SHA here so the build is
# keyed on the commit.
#
#   downstream/build.sh                              # the pinned commits
#   KUBERNETES_REF=my-branch downstream/build.sh     # one repository elsewhere
#   AWS_REPO=https://github.com/me/mgtt-provider-aws.git AWS_REF=fix downstream/build.sh
#   downstream/build.sh --no-cache                   # extra args go to docker build
#   downstream/build.sh --print-args                 # NAME=VALUE lines, no build (CI)
#   downstream/build.sh --bump                       # rewrite every pin to its main
#
# The repositories are read from the NAME_REPO / NAME_REF ARG lines in
# downstream/Dockerfile.
set -eu
cd "$(dirname "$0")/.."

dockerfile=downstream/Dockerfile
tag=${DOWNSTREAM_TAG:-mgtt-downstream}
mode=build
case "${1:-}" in
--print-args) mode=print; shift ;;
--bump) mode=bump; shift ;;
esac
set -- -f "$dockerfile" -t "$tag" "$@"

resolve() { # REPO REF -> SHA
  if printf '%s' "$2" | grep -qE '^[0-9a-f]{40}$'; then
    printf '%s' "$2"
    return
  fi
  GIT_TERMINAL_PROMPT=0 git ls-remote "$1" "refs/heads/$2" "refs/tags/$2" | head -n 1 | cut -f1
}

for name in $(sed -n 's/^ARG \([A-Z0-9]*\)_REPO=.*/\1/p' "$dockerfile"); do
  pinned_repo=$(sed -n "s/^ARG ${name}_REPO=//p" "$dockerfile")
  pinned_ref=$(sed -n "s/^ARG ${name}_REF=//p" "$dockerfile")
  repo=$(eval "printf '%s' \"\${${name}_REPO:-}\"")
  [ -n "$repo" ] || repo=$pinned_repo
  ref=$(eval "printf '%s' \"\${${name}_REF:-}\"")
  if [ "$mode" = bump ]; then ref=main; fi
  [ -n "$ref" ] || ref=$pinned_ref
  sha=$(resolve "$repo" "$ref")
  [ -n "$sha" ] || { echo "downstream: no branch or tag '$ref' in $repo" >&2; exit 2; }
  printf 'downstream: %-11s %s  %s (%s)\n' "$name" "$sha" "$repo" "$ref" >&2
  case "$mode" in
  print) printf '%s_REPO=%s\n%s_REF=%s\n' "$name" "$repo" "$name" "$sha" ;;
  bump) sed -i "s|^ARG ${name}_REF=.*|ARG ${name}_REF=${sha}|" "$dockerfile" ;;
  esac
  set -- "$@" --build-arg "${name}_REPO=$repo" --build-arg "${name}_REF=$sha"
done
case "$mode" in
print) exit 0 ;;
bump) echo "downstream: pins rewritten in $dockerfile; review with git diff" >&2; exit 0 ;;
esac

exec docker build "$@" .
