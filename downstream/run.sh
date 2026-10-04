#!/bin/sh
# Copyright (C) 2026 Alex Kunich
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Run the downstream suites against the `mgtt` on PATH and the providers in
# $MGTT_HOME. Each suite exits 0 when the claim it names holds.
#
#   run.sh list        the suites, one per line
#   run.sh NAME        one suite
#   run.sh all         every suite; exits 0 only if every one passed (default)
#
# A suite named in xfail (next to this script) is a known breakage. It counts
# as passed while it fails (XFAIL) and as failed once it passes (XPASS), so the
# fix that makes it pass has to remove it from the list in the same change.
set -u

root=${DOWNSTREAM:-/downstream}
xfail_file=$root/xfail
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

providers="aws docker kubernetes quickwit tempo terraform"

# Every provider built against this engine is installed and listed.
suite_providers() {
  mgtt provider ls >"$tmp/ls" || return 1
  for p in $providers; do
    grep -q "$p" "$tmp/ls" || { echo "provider $p not installed"; cat "$tmp/ls"; return 1; }
  done
}

# One provider's unit tests passed against this engine, and its static checks
# pass.
suite_provider() {
  status=$(cat "$MGTT_HOME/results/$1.status" 2>/dev/null || echo missing)
  if [ "$status" != pass ]; then
    echo "unit tests: $status"
    tail -n 20 "$MGTT_HOME/results/$1.log" 2>/dev/null
    return 1
  fi
  mgtt provider validate "$1"
}

# The README's contract: the clean model verifies with exit 0 over six
# configurations; the drifted one is caught with exit 1 in two.
suite_minishop() {
  cp -r "$root/examples/minishop" "$tmp/minishop"
  cd "$tmp/minishop" || return 1
  export MGTT_HOME="$PWD/mgtt-home"
  mgtt model export --json model.yaml | mgtt2writ | writ check --stdin >"$tmp/clean"
  rc=$?
  cat "$tmp/clean"
  [ "$rc" -eq 0 ] || { echo "clean model: exit $rc, want 0"; return 1; }
  grep -q 'states: 6' "$tmp/clean" || { echo "clean model: want 'states: 6'"; return 1; }
  mgtt model export --json model-drifted.yaml | mgtt2writ | writ check --stdin >"$tmp/drifted"
  rc=$?
  cat "$tmp/drifted"
  [ "$rc" -eq 1 ] || { echo "drifted model: exit $rc, want 1"; return 1; }
  grep -q 'violated in 2' "$tmp/drifted" || { echo "drifted model: want 'violated in 2'"; return 1; }
}

# The flagship example validates and its five documented scenarios pass.
suite_storefront() {
  cd "$root/examples/storefront" || return 1
  mgtt model validate system.model.yaml || return 1
  mgtt simulate --model system.model.yaml --all --scenarios-dir scenarios
}

# Scenario-guided diagnosis on the flagship example decides a probe within the
# budget (seconds). G2: enumerate, then one occam decision.
suite_storefront_speed() {
  budget=${STOREFRONT_PROBE_BUDGET:-10}
  sed '/^  scenarios: none/d' "$root/examples/storefront/system.model.yaml" >"$tmp/system.model.yaml"
  cd "$tmp" || return 1
  mgtt model validate system.model.yaml --write-scenarios | tail -n 1 || return 1
  if ! timeout "$budget" mgtt simulate --model system.model.yaml --fuzz 1 --fuzz-seed 1; then
    echo "one probe decision did not finish within ${budget}s"
    return 1
  fi
}

# End to end on the fixture the translator pins. Exit 77 is a failure here.
suite_mgtt2writ() {
  (cd "$root/mgtt2writ" && MGTT2WRIT=mgtt2writ sh test/pipeline.sh)
}

# The MCP server answers over stdio, and every tool name is portable: some
# clients and model APIs accept only [A-Za-z0-9_-], 1 to 64 characters.
suite_mcp() {
  printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"downstream","version":"0"}}}' \
    '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
    '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
    | timeout 20 mgtt mcp serve 2>"$tmp/mcp.err" >"$tmp/mcp.out"
  grep '"id":2' "$tmp/mcp.out" | grep -o '"name":"[^"]*"' | sed 's/"name":"//; s/"$//' >"$tmp/tools"
  [ -s "$tmp/tools" ] || { echo "no tools listed"; cat "$tmp/mcp.out" "$tmp/mcp.err"; return 1; }
  cat "$tmp/tools"
  bad=$(grep -Ev '^[A-Za-z0-9_-]{1,64}$' "$tmp/tools")
  [ -z "$bad" ] || { echo "non-portable tool names:"; echo "$bad"; return 1; }
}

# await_reply ID waits (up to 30s) until the MCP session's output holds the
# reply to request ID.
await_reply() {
  n=0
  until grep -q "\"id\":$1[,}]" "$tmp/mcp.out" 2>/dev/null; do
    n=$((n + 1))
    [ "$n" -le 300 ] || return 0
    sleep 0.1
  done
}

# MCP runs a fact with no probe.cmd through its provider's runner binary,
# as the CLI does, instead of handing it to a human. aws declares no cmd
# at all. With no aws CLI in the image the runner fails, which is fine:
# the claim is that it ran.
suite_mcp_probe() {
  mkdir -p "$tmp/awsprobe" && cd "$tmp/awsprobe" || return 1
  printf '%s\n' 'meta:' '  name: awsprobe' '  version: "1.0"' '  providers: [aws]' \
    'components:' '  db:' '    type: rds_instance' '    resource: downstream-db' >system.model.yaml
  # The server answers requests concurrently, so each call waits for the
  # reply it depends on, as a real client does.
  : >"$tmp/mcp.out"
  {
    printf '%s\n' \
      '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"downstream","version":"0"}}}' \
      '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
      '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"incident.start","arguments":{"model_ref":"system.model.yaml","id":"inc-downstream"}}}'
    await_reply 2
    printf '%s\n' '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"probe","arguments":{"incident_id":"inc-downstream","execute":true}}}'
    await_reply 3
  } | timeout 60 mgtt mcp serve 2>"$tmp/mcp.err" >"$tmp/mcp.out"
  status=$(grep '"id":3' "$tmp/mcp.out" | grep -o '\\"status\\":\\"[a-z_]*' | sed 's/.*"//')
  echo "probe status: ${status:-<none>}"
  case "$status" in
  executed | not_found | forbidden | transient | error) ;;
  *) cat "$tmp/mcp.out" "$tmp/mcp.err"; return 1 ;;
  esac
}

suites="providers"
for p in $providers; do suites="$suites provider-$p"; done
suites="$suites minishop storefront storefront-speed mgtt2writ mcp mcp-probe"

# Suites are suite_* functions, so none can shadow the tool it runs.
dispatch() {
  case "$1" in
  provider-*) suite_provider "${1#provider-}" ;;
  *) "suite_$(printf '%s' "$1" | tr - _)" ;;
  esac
}

xfail_reason() {
  [ -f "$xfail_file" ] || return 1
  line=$(grep -E "^$1[[:space:]]" "$xfail_file") || return 1
  printf '%s' "$line" | sed -E 's/^[^[:space:]]+[[:space:]]+//'
}

run_one() {
  printf '\n######## %s ########\n' "$1"
  ( dispatch "$1" )
  rc=$?
  if reason=$(xfail_reason "$1"); then
    if [ "$rc" -eq 0 ]; then
      printf '######## %s: XPASS (listed in xfail: %s) -- remove it from downstream/xfail\n' "$1" "$reason"
      return 1
    fi
    printf '######## %s: XFAIL (%s)\n' "$1" "$reason"
    return 0
  fi
  if [ "$rc" -eq 0 ]; then
    printf '######## %s: PASS\n' "$1"
  else
    printf '######## %s: FAIL (exit %s)\n' "$1" "$rc"
  fi
  return "$rc"
}

sel=${1:-all}
case "$sel" in
list) printf '%s\n' $suites ;;
all)
  mgtt version
  failed=""
  for s in $suites; do run_one "$s" || failed="$failed $s"; done
  echo
  if [ -n "$failed" ]; then
    echo "downstream: FAILED:$failed"
    exit 1
  fi
  echo "downstream: all suites passed"
  ;;
*)
  case " $suites " in
  *" $sel "*) run_one "$sel" ;;
  *) echo "unknown suite: $sel (try: list)" >&2; exit 2 ;;
  esac
  ;;
esac
