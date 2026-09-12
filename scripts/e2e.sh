#!/usr/bin/env bash
# e2e.sh — binary-level end-to-end checks against the real Brave Search API.
#
# The Go suite in e2e/ exercises the packages; this exercises the thing a user
# actually runs: the built binary, its exit codes, its stdout/stderr split, and
# the MCP stdio session. Those are contracts a package-level test cannot see.
#
# Network and a configured key required. Run via `make e2e`, which builds first.
#
# Budget: four billed requests (web ×2, context, one MCP web_search). Everything
# else here is refused locally or by an unbilled 4xx.

set -uo pipefail

BIN="${BIN:-dist/brave-search}"
[ -x "$BIN" ] || { echo "FAIL: $BIN not built (run make build)"; exit 1; }
BIN="$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN")"

pass=0; fail=0
ok()   { printf '  ok   %s\n' "$1"; pass=$((pass+1)); }
bad()  { printf '  FAIL %s\n' "$1"; fail=$((fail+1)); }
skip() { printf '  skip %s\n' "$1"; }

# check NAME EXPECTED_EXIT -- command...
check() {
  local name=$1 want=$2; shift 3
  local out; out=$("$@" 2>&1); local got=$?
  if [ "$got" = "$want" ]; then ok "$name"; else
    bad "$name (exit $got, want $want)"
    printf '       %s\n' "$(printf '%s' "$out" | head -3)"
  fi
}

# contains NAME NEEDLE -- command...
contains() {
  local name=$1 needle=$2; shift 3
  local out; out=$("$@" 2>/dev/null)
  if printf '%s' "$out" | grep -q -- "$needle"; then ok "$name"; else
    bad "$name (no '$needle')"; printf '       %s\n' "$(printf '%s' "$out" | head -3)"
  fi
}

echo "== contracts that need no key"
check    "--version exits 0"                    0 -- "$BIN" --version
contains "--version names the binary"           "brave-search" -- "$BIN" --version
check    "help exits 0"                         0 -- "$BIN" help
check    "no args is a usage error"             2 -- "$BIN"
check    "unknown command is a usage error"     2 -- "$BIN" frobnicate
check    "web without a query is a usage error" 2 -- "$BIN" web
check    "bad count is refused locally"         2 -- "$BIN" web q --count 99
check    "bad freshness is refused locally"     2 -- "$BIN" context q --freshness yesterday

# Missing key: isolate the config so the operator's own key is not read.
NOKEY="$(mktemp -d)"
check "missing key is a usage error, nothing sent" 2 -- env -i HOME="$NOKEY" XDG_CONFIG_HOME="$NOKEY/x" PATH="$PATH" "$BIN" web q
rm -rf "$NOKEY"

echo "== auth check (unbilled probes)"
if "$BIN" auth check --json >/dev/null 2>&1; then
  ok "auth check reports both plans valid"
  HAVE_KEY=1
else
  skip "auth check did not report both plans valid — billed checks below are skipped"
  "$BIN" auth check 2>&1 | sed 's/^/       /'
  HAVE_KEY=0
fi

if [ "$HAVE_KEY" = 1 ]; then
  echo "== billed checks (3 requests)"
  contains "web prints a ranked result"       "1. "            -- "$BIN" web "Brave Search API" --count 2
  contains "web --json carries cost_usd"      "cost_usd"       -- "$BIN" web "Brave Search API" --count 2 --json
  contains "context returns chunks"           '"snippets":'    -- "$BIN" context "Brave Search API" --count 2 --max-tokens 1024 --json

  echo "== MCP stdio session (1 request)"
  SESSION=$(printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}' \
    '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
    '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
    '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_usage"}}' \
    '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"web_search","arguments":{"query":"Brave Search API","count":1}}}' \
    '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"web_search","arguments":{"query":"x","count":99}}}' \
    | "$BIN" mcp 2>/dev/null)
  if [ "$(printf '%s\n' "$SESSION" | wc -l | tr -d ' ')" = 5 ]; then ok "5 responses to 5 requests (notification silent)"; else bad "response count"; fi
  printf '%s' "$SESSION" | sed -n 1p | grep -q '"brave-search"'      && ok "initialize names the server"   || bad "initialize"
  printf '%s' "$SESSION" | sed -n 2p | grep -q '"research"'          && ok "tools/list has research"       || bad "tools/list"
  printf '%s' "$SESSION" | sed -n 3p | grep -q 'EVERY CALL\|billed'  && ok "get_usage returns the manual"  || bad "get_usage"
  printf '%s' "$SESSION" | sed -n 4p | grep -q 'cost_usd'            && ok "web_search carries meta"       || bad "web_search"
  printf '%s' "$SESSION" | sed -n 5p | grep -q 'invalid_arguments'   && ok "bad count is a structured error" || bad "bad count"
fi

echo
echo "passed: $pass  failed: $fail"
[ "$fail" = 0 ]
