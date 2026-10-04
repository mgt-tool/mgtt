#!/bin/sh
# Copyright (C) 2026 Alex Kunich
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Build one provider against the mgtt checkout at /src/mgtt and install it into
# $MGTT_HOME. Runs inside the downstream image's providers stage.
#
#   provider.sh NAME REPO REF
#
# Cloning, building and installing must succeed. The unit-test outcome is
# recorded in $MGTT_HOME/results/NAME.{status,log} for the provider-NAME suite
# to assert on, so one provider's known test breakage stays visible without
# stopping every other provider from being checked.
set -eu

name=$1 repo=$2 ref=$3
dir=/build/mgtt-provider-$name
results=$MGTT_HOME/results
mkdir -p "$results"

git clone -q "$repo" "$dir"
cd "$dir"
git checkout -q "$ref"
git log --oneline -1

# Point the provider at the engine under test, not the release it pins.
go mod edit -replace github.com/mgt-tool/mgtt=/src/mgtt
go mod tidy

if go vet ./... >"$results/$name.log" 2>&1 && go test ./... >>"$results/$name.log" 2>&1; then
  echo pass >"$results/$name.status"
else
  echo fail >"$results/$name.status"
fi
echo "provider $name: unit tests $(cat "$results/$name.status")"

mgtt provider install "$dir"
