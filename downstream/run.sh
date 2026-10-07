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
  mgtt simulate --model system.model.yaml --all --scenarios-dir scenarios || return 1
  # Drafted scenarios, written out and read back, pass as written; and the
  # engine names every drafted failure from where it is seen.
  mgtt simulate --model system.model.yaml --suggest --write --scenarios-dir "$tmp/drafts" 2>"$tmp/drafts.txt" >/dev/null || return 1
  cat "$tmp/drafts.txt"
  grep -q ", 0 marked REVIEW" "$tmp/drafts.txt" || { echo "a drafted failure is not named from where it is seen"; return 1; }
  mgtt simulate --model system.model.yaml --all --scenarios-dir "$tmp/drafts" | tail -n 1
}

# A redundancy group (need: 1 of 2 webs) on the real kubernetes types:
# one member down is degraded, not a root cause; a cause under both is.
suite_redundant_web() {
  cd "$root/examples/redundant-web" || return 1
  mgtt model validate system.model.yaml || return 1
  mgtt simulate --model system.model.yaml --all --scenarios-dir scenarios || return 1
  # The blast radius agrees: one web down is stopped by the group, the
  # database under both reaches the Service.
  mgtt model impact web-a --model system.model.yaml | tee "$tmp/impact"
  grep -q "shop-svc holds: redundancy covers web-a" "$tmp/impact" || { echo "web-a: group should hold"; return 1; }
  mgtt model impact db --model system.model.yaml | grep -q "! shop-svc" || { echo "db: should reach shop-svc"; return 1; }
  # And the diff that removes the group says what that does for users.
  sed '/need: 1/d' system.model.yaml >"$tmp/hard.yaml"
  mgtt model diff system.model.yaml "$tmp/hard.yaml" | tee "$tmp/diff"
  grep -q "web-a fails: now reaches shop-svc" "$tmp/diff" || { echo "diff: removing the group should newly expose shop-svc"; return 1; }
  # Drafted scenarios: one web down is the redundancy archetype, the
  # database under both is the root cause.
  mgtt simulate --model system.model.yaml --suggest --component web-a 2>/dev/null >"$tmp/web-a.yaml" || return 1
  grep -q "redundancy_degraded: \[web-a\]" "$tmp/web-a.yaml" && grep -q "root_cause: none" "$tmp/web-a.yaml" ||
    { echo "suggest web-a: want the group to hold"; cat "$tmp/web-a.yaml"; return 1; }
  mgtt simulate --model system.model.yaml --suggest --component db 2>/dev/null | grep -q "root_cause: db" ||
    { echo "suggest db: want db named"; return 1; }
  # And writ, exhaustively, on the same model and types: in every reachable
  # situation the health rules agree with the states, and the group holds
  # there as it does in simulate. Walked by move name from the initial
  # situation, so nothing about how facts become cells is assumed.
  mgtt model export --json system.model.yaml | mgtt2writ >"$tmp/rw.writ" || return 1
  writ check "$tmp/rw.writ" --no-certificate >"$tmp/rw.check" || { cat "$tmp/rw.check"; echo "writ: a law fails"; return 1; }
  head -n 1 "$tmp/rw.check"
  a=$(writ_after "$tmp/rw.writ" 0 web-a-fails-crashed)
  [ -n "$a" ] || { echo "writ: web-a cannot crash"; return 1; }
  if writ_offers "$tmp/rw.writ" "$a" web-a-crashed-triggers-shop-svc-no-endpoints; then
    echo "writ: one web down takes shop-svc down"
    return 1
  fi
  d=$(writ_after "$tmp/rw.writ" 0 db-fails-crashed)
  da=$(writ_after "$tmp/rw.writ" "$d" db-crashed-triggers-web-a-crashed)
  dab=$(writ_after "$tmp/rw.writ" "$da" db-crashed-triggers-web-b-crashed)
  [ -n "$dab" ] || { echo "writ: db cannot push both webs over"; return 1; }
  writ_offers "$tmp/rw.writ" "$dab" web-a-crashed-triggers-shop-svc-no-endpoints ||
    { echo "writ: db under both webs does not reach shop-svc"; return 1; }
}

# writ_after MODEL N MOVE: the situation MOVE leads to from situation N, or
# nothing when MOVE is not enabled there.
writ_after() {
  writ show "$1" --at "$2" | sed -n '/moves:/,$p' | tr -s ' \n' '\n\n' |
    awk -v mv="$3" '$0 == mv { getline; getline; print; exit }'
}

# writ_offers MODEL N MOVE: whether MOVE is enabled in situation N.
writ_offers() {
  writ show "$1" --at "$2" | sed -n '/moves:/,$p' | tr -s ' \n' '\n\n' | grep -qx "$3"
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

# End to end on the fixtures the translator pins, except that its
# redundancy-group checks read a fresh export from this mgtt: one store of two
# down must not take api down in writ, both must. Exit 77 is a failure here.
suite_mgtt2writ() {
  MGTT_HOME="$root/examples/minishop/mgtt-home" mgtt model export --json \
    "$root/mgtt2writ/test/fixtures/mgtt-export-group.yaml" >"$tmp/group.json" || return 1
  (cd "$root/mgtt2writ" && MGTT2WRIT=mgtt2writ GROUP_FIXTURE="$tmp/group.json" sh test/pipeline.sh)
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
      '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"incident_start","arguments":{"model_ref":"system.model.yaml","id":"inc-downstream"}}}'
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

# The authoring toolset against the real providers: it serves no incident
# tool, describes a real type, validates the storefront model and runs all
# its scenarios sent inline -- as a client that cannot place files on the
# server would -- and serves the guide.
# Scenario listings stay readable on the flagship: one representative per
# class with its count, a page at a time, the totals beside them, and a
# snapshot that does not carry every chain.
suite_mcp_scenarios() {
  : >"$tmp/mcp.out"
  {
    printf '%s\n' \
      '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"downstream","version":"0"}}}' \
      '{"jsonrpc":"2.0","method":"notifications/initialized"}'
    printf '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"incident_start","arguments":{"model_ref":"%s","id":"inc-scenarios"}}}\n' \
      "$root/examples/storefront/system.model.yaml"
    await_reply 2
    printf '%s\n' \
      '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"scenarios_list","arguments":{"incident_id":"inc-scenarios"}}}' \
      '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"scenarios_list","arguments":{"incident_id":"inc-scenarios","page_token":"50"}}}' \
      '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"incident_snapshot","arguments":{"incident_id":"inc-scenarios"}}}'
    await_reply 3
    await_reply 4
    await_reply 5
  } | timeout 120 mgtt mcp serve 2>"$tmp/mcp.err" >"$tmp/mcp.out"
  num() { grep "\"id\":$1[,}]" "$tmp/mcp.out" | grep -o "\\\\\"$2\\\\\":[0-9]*" | head -n 1 | grep -o '[0-9]*$'; }
  chains=$(num 3 chains) total=$(num 3 total)
  echo "storefront: ${chains:-?} chains in ${total:-?} classes; first page $(grep '"id":3[,}]' "$tmp/mcp.out" | wc -c) bytes, snapshot $(grep '"id":5[,}]' "$tmp/mcp.out" | wc -c) bytes"
  [ -n "$chains" ] && [ -n "$total" ] && [ "$total" -lt "$chains" ] ||
    { echo "want fewer classes than chains"; grep '"id":3[,}]' "$tmp/mcp.out" | cut -c1-400; cat "$tmp/mcp.err"; return 1; }
  if [ "$total" -gt 50 ]; then
    grep '"id":3[,}]' "$tmp/mcp.out" | grep -q '\\"next_page_token\\":\\"50\\"' || { echo "the first page should continue at 50"; return 1; }
    [ "$(num 4 total)" = "$total" ] || { echo "the second page should list the same classes"; return 1; }
  fi
  [ "$(num 5 surviving_chains)" = "$chains" ] || { echo "the snapshot should count every chain as surviving"; return 1; }
  [ "$(grep '"id":5[,}]' "$tmp/mcp.out" | wc -c)" -lt 50000 ] || { echo "the snapshot should stay under 50 KB"; return 1; }
  [ "$(grep '"id":5[,}]' "$tmp/mcp.out" | grep -o '\\"count\\":' | wc -l)" -le 50 ] ||
    { echo "the snapshot should list at most 25 classes a side"; return 1; }
}

suite_mcp_authoring() {
  src=$(awk '{ gsub(/\\/, "\\\\"); gsub(/"/, "\\\""); printf "%s\\n", $0 }' "$root/examples/storefront/system.model.yaml")
  # Every storefront scenario, as one inline source separated by ---.
  scs=$(for f in "$root"/examples/storefront/scenarios/*.yaml; do cat "$f"; echo '---'; done \
    | awk '{ gsub(/\\/, "\\\\"); gsub(/"/, "\\\""); printf "%s\\n", $0 }')
  : >"$tmp/mcp.out"
  {
    printf '%s\n' \
      '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"downstream","version":"0"}}}' \
      '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
      '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
      '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"types_describe","arguments":{"type":"deployment"}}}'
    printf '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"model_validate","arguments":{"model_source":"%s"}}}\n' "$src"
    printf '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"scenario_simulate","arguments":{"model_source":"%s","scenarios_source":"%s"}}}\n' "$src" "$scs"
    printf '%s\n' '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"guide","arguments":{}}}'
    printf '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"scenario_suggest","arguments":{"model_source":"%s","limit":2}}}\n' "$src"
    printf '%s\n' '{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"types_describe","arguments":{"type":"mq_broker"}}}'
    await_reply 4
    await_reply 5
    await_reply 6
    await_reply 7
    await_reply 8
  } | timeout 60 mgtt mcp serve --toolset authoring 2>"$tmp/mcp.err" >"$tmp/mcp.out"
  grep '"id":2' "$tmp/mcp.out" | grep -q '"name":"model_validate"' || { echo "model_validate not listed"; return 1; }
  if grep '"id":2' "$tmp/mcp.out" | grep -q '"name":"incident_start"'; then echo "authoring toolset serves incident_start"; return 1; fi
  grep '"id":3' "$tmp/mcp.out" | grep -q 'ready_replicas' || { echo "types_describe deployment lacks ready_replicas"; cat "$tmp/mcp.out" "$tmp/mcp.err"; return 1; }
  grep '"id":4' "$tmp/mcp.out" | grep -q '\\"ok\\":true' || { echo "storefront model_source did not validate ok"; grep '"id":4' "$tmp/mcp.out" | cut -c1-600; cat "$tmp/mcp.err"; return 1; }
  grep '"id":5' "$tmp/mcp.out" | grep -q '\\"failed\\":0' || { echo "storefront scenarios did not all pass inline"; grep '"id":5' "$tmp/mcp.out" | cut -c1-600; return 1; }
  grep '"id":6' "$tmp/mcp.out" | grep -q 'authoring loop' || { echo "guide index missing"; return 1; }
  # A provider's derived fact reaches the vocabulary with its window.
  grep '"id":8' "$tmp/mcp.out" | grep -q '\\"name\\":\\"queue_depth_delta_5m\\"[^}]*\\"window\\":\\"5m\\",\\"derive\\":\\"delta\\"' ||
    { echo "types_describe mq_broker: want queue_depth_delta_5m over 5m by delta"; grep '"id":8' "$tmp/mcp.out" | cut -c1-800; return 1; }
  grep '"id":7' "$tmp/mcp.out" | grep -q '\\"next_page_token\\":\\"2\\"' ||
    { echo "scenario_suggest should page its drafts"; grep '"id":7' "$tmp/mcp.out" | cut -c1-600; cat "$tmp/mcp.err"; return 1; }
  echo "authoring toolset: listed, described, validated, simulated ($(grep '"id":5' "$tmp/mcp.out" | grep -o 'passed[^,]*' | tr -d '\\"')), guided, drafted ($(grep '"id":7' "$tmp/mcp.out" | grep -o 'total[^,]*' | head -n 1 | tr -d '\\"'))"
}

# An agent with no shell composes the tools as the minishop pipe does: mgtt's
# model_export, then mgtt2writ's mgtt_to_writ with the document exactly as it
# came back, then writ on the model file that wrote. The clean model must
# certify, as it does through the pipe.
suite_mcp_chain() {
  printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"downstream","version":"0"}}}' \
    '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
    "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"model_export\",\"arguments\":{\"model_path\":\"$root/examples/minishop/model.yaml\"}}}" \
    | MGTT_HOME="$root/examples/minishop/mgtt-home" timeout 30 mgtt mcp serve --toolset authoring 2>"$tmp/mcp.err" >"$tmp/mcp.out"
  # The tool's text, still a JSON string literal: handed on, never decoded.
  doc=$(grep '"id":2[,}]' "$tmp/mcp.out" | sed -n 's/.*"text":\("\([^"\\]\|\\.\)*"\).*/\1/p')
  [ -n "$doc" ] || { echo "model_export answered no document"; cat "$tmp/mcp.out" "$tmp/mcp.err"; return 1; }
  printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"downstream","version":"0"}}}' \
    "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"mgtt_to_writ\",\"arguments\":{\"export\":$doc,\"out_dir\":\"$tmp/chain\"}}}" \
    | timeout 30 mgtt2writ mcp >"$tmp/m2w.out"
  [ -s "$tmp/chain/minishop.writ" ] || { echo "mgtt_to_writ wrote no model"; cat "$tmp/m2w.out"; return 1; }
  writ check "$tmp/chain/minishop.writ" >"$tmp/chain.check" ||
    { echo "writ rejected the chained model"; cat "$tmp/chain.check"; return 1; }
  grep -q '^certified' "$tmp/chain.check" || { echo "the chained model did not certify"; cat "$tmp/chain.check"; return 1; }
  echo "model_export -> mgtt_to_writ -> writ check: certified"
}

suites="providers"
for p in $providers; do suites="$suites provider-$p"; done
suites="$suites minishop storefront storefront-speed redundant-web mgtt2writ mcp mcp-probe mcp-authoring mcp-scenarios mcp-chain"

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
