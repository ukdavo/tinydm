#!/usr/bin/env bash
# test_phase5.sh — Integration tests for TinyDM Phase 5: Audit Log.
#
# Covers:
#   • Mutating requests (POST/PUT/DELETE 2xx) are recorded in the audit log
#   • Read-only requests (GET) are NOT recorded
#   • Failed requests (4xx) are NOT recorded
#   • Filtering by action (exact and wildcard *)
#   • Filtering by principal (username)
#   • Filtering by resource (document ID)
#   • Date-range filters (from / to)
#   • Pagination (limit / offset)
#   • Unauthenticated access to audit endpoint returns 401
#   • Non-admin access to audit endpoint returns 403
#
# Prerequisites: curl, python3
# The server must be running before executing this script.
#
# Usage:
#   ./test_phase5.sh [BASE_URL]
#   BASE_URL defaults to http://localhost:8080

set -eo pipefail

BASE_URL="${1:-${TINYDM_URL:-http://localhost:8080}}"
ADMIN_USER="${TINYDM_ADMIN_USER:-admin}"
ADMIN_PASS="${TINYDM_ADMIN_PASS:-changeme}"
TENANT_ID="default"

# ── Colours ───────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; DIM='\033[2m'; NC='\033[0m'

PASS=0; FAIL=0

pass()    { echo -e "    ${GREEN}✓${NC} $1"; PASS=$((PASS + 1)); }
fail()    { echo -e "    ${RED}✗${NC} $1"; FAIL=$((FAIL + 1)); }
section() { echo -e "\n${CYAN}${BOLD}── $1${NC}"; }
info()    { echo -e "    ${DIM}$1${NC}"; }

# ── Assertions ────────────────────────────────────────────────────────────────
assert_eq() {
    local got="$1" want="$2" msg="$3"
    if [[ "$got" == "$want" ]]; then
        pass "$msg"
    else
        fail "$msg"
        echo -e "      ${DIM}want: $want${NC}"
        echo -e "      ${DIM} got: $got${NC}"
    fi
}

assert_ge() {
    local got="$1" want="$2" msg="$3"
    if [[ "$got" -ge "$want" ]]; then
        pass "$msg"
    else
        fail "$msg"
        echo -e "      ${DIM}want: >= $want${NC}"
        echo -e "      ${DIM} got: $got${NC}"
    fi
}

assert_contains() {
    local body="$1" needle="$2" msg="$3"
    if echo "$body" | grep -qF "$needle"; then
        pass "$msg"
    else
        fail "$msg"
        echo -e "      ${DIM}expected to find: $needle${NC}"
        echo -e "      ${DIM}in: ${body:0:400}${NC}"
    fi
}

assert_not_contains() {
    local body="$1" needle="$2" msg="$3"
    if ! echo "$body" | grep -qF "$needle"; then
        pass "$msg"
    else
        fail "$msg"
        echo -e "      ${DIM}did not expect: $needle${NC}"
        echo -e "      ${DIM}in: ${body:0:400}${NC}"
    fi
}

# ── HTTP helpers ──────────────────────────────────────────────────────────────
# Every request prints "  METHOD  path                         STATUS" to stderr.
# Body-returning helpers use a temp file so there is no string-parsing involved
# in separating the response body from the status code.
AUTH_ARGS=()

_show_req() {
    local method="$1" path="$2" sc="$3"
    local colour="$GREEN"
    [[ "$sc" -ge 400 ]] 2>/dev/null && colour="$RED"
    [[ "$sc" -ge 300 && "$sc" -lt 400 ]] 2>/dev/null && colour="$YELLOW"
    local short="${path:0:60}"
    [[ "${#path}" -gt 60 ]] && short="${path:0:57}..."
    printf "  ${DIM}%-10s %-62s${NC} ${colour}%s${NC}\n" "$method" "$short" "$sc" >&2
}

# Core helper: write body to a temp file, return status code in $sc_out.
# Usage: _curl_req SC_VAR METHOD PATH [extra curl args...]
# Caller reads the temp file name from $CURL_TMP (set by this function).
CURL_TMP=""
_curl_req() {
    local _sc_var="$1" _method="$2" _path="$3"; shift 3
    CURL_TMP=$(mktemp)
    local _sc
    _sc=$(curl -s -o "$CURL_TMP" -w "%{http_code}" \
        -X "$_method" ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$@" "$BASE_URL$_path")
    _show_req "$_method" "$_path" "$_sc"
    eval "$_sc_var=$_sc"
}

# Body-returning helpers — fail (return 1) on 4xx/5xx.
_get() {
    local _sc
    _curl_req _sc GET "$1"
    if [[ "$_sc" -ge 400 ]]; then rm -f "$CURL_TMP"; return 1; fi
    cat "$CURL_TMP"; rm -f "$CURL_TMP"
}

_post() {
    local _sc
    _curl_req _sc POST "$1" -H "Content-Type: application/json" -d "$2"
    if [[ "$_sc" -ge 400 ]]; then rm -f "$CURL_TMP"; return 1; fi
    cat "$CURL_TMP"; rm -f "$CURL_TMP"
}

_put() {
    local _sc
    _curl_req _sc PUT "$1" -H "Content-Type: application/json" -d "$2"
    if [[ "$_sc" -ge 400 ]]; then rm -f "$CURL_TMP"; return 1; fi
    cat "$CURL_TMP"; rm -f "$CURL_TMP"
}

_post_mp() {
    local _path="$1"; shift
    local _sc
    _curl_req _sc POST "$_path" "$@"
    if [[ "$_sc" -ge 400 ]]; then rm -f "$CURL_TMP"; return 1; fi
    cat "$CURL_TMP"; rm -f "$CURL_TMP"
}

_put_mp() {
    local _path="$1"; shift
    local _sc
    _curl_req _sc PUT "$_path" "$@"
    if [[ "$_sc" -ge 400 ]]; then rm -f "$CURL_TMP"; return 1; fi
    cat "$CURL_TMP"; rm -f "$CURL_TMP"
}

_post_tag() {
    # POST to a tag path (no request body).
    local _sc
    _curl_req _sc POST "$1"
    if [[ "$_sc" -ge 400 ]]; then rm -f "$CURL_TMP"; return 1; fi
    cat "$CURL_TMP"; rm -f "$CURL_TMP"
}

# Status-code-only variants — never fail on 4xx/5xx.
_sc() {
    local sc
    sc=$(curl -s -o /dev/null -w "%{http_code}" \
        ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$BASE_URL$1")
    _show_req GET "$1" "$sc"
    echo "$sc"
}

_sc_delete() {
    local sc
    sc=$(curl -s -o /dev/null -w "%{http_code}" \
        -X DELETE ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$BASE_URL$1")
    _show_req DELETE "$1" "$sc"
    echo "$sc"
}

_sc_noauth() {
    local sc
    sc=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL$1")
    _show_req "GET(noauth)" "$1" "$sc"
    echo "$sc"
}

_sc_bearer() {
    local tok="$1" path="$2" sc
    sc=$(curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer $tok" "$BASE_URL$path")
    _show_req "GET(user)" "$path" "$sc"
    echo "$sc"
}

jfield() { python3 -c "import sys,json; print(json.load(sys.stdin)$1)" 2>/dev/null; }
# Envelope-aware helpers: unwrap {"data":[...]} paginated responses automatically.
jlen()  { python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d['data'] if isinstance(d,dict) and 'data' in d else d))" 2>/dev/null; }
jdata() { python3 -c "import sys,json; d=json.load(sys.stdin); import json as j; print(j.dumps(d['data'] if isinstance(d,dict) and 'data' in d else d))" 2>/dev/null; }

# Helper: query the audit log.  Pass optional query string, e.g. "?action=foo".
audit() { _get "/api/v1/tenants/$TENANT_ID/audit$1"; }

# ── Prerequisites ─────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}TinyDM — Phase 5 Audit Log Integration Tests${NC}"
echo    "Server : $BASE_URL"
echo    "Tenant : $TENANT_ID"

for cmd in curl python3; do
    if ! command -v "$cmd" &>/dev/null; then
        echo -e "${RED}Required tool not found: $cmd${NC}"; exit 1
    fi
done

echo ""
echo -e "  Checking server health…"
health=$(curl -sf "$BASE_URL/health" 2>/dev/null || true)
if [[ "$health" != *'"ok"'* ]]; then
    echo -e "  ${RED}Server not reachable at $BASE_URL — run ./run.sh first.${NC}"; exit 1
fi
echo -e "  ${GREEN}Server is up.${NC}"

# ── Temp workspace ────────────────────────────────────────────────────────────
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

printf 'Audit test document — alpha\n' > "$WORK/alpha.txt"
printf 'Audit test document — beta\n'  > "$WORK/beta.txt"

# ─────────────────────────────────────────────────────────────────────────────
section "Authentication"

echo ""
echo -e "  Logging in as '$ADMIN_USER'…"
_login_json=$(printf '{"tenant_id":"%s","username":"%s","password":"%s"}' \
    "$TENANT_ID" "$ADMIN_USER" "$ADMIN_PASS")
_login_file=$(mktemp)
_login_sc=$(curl -s -o "$_login_file" -w "%{http_code}" \
    -X POST \
    -H "Content-Type: application/json" \
    -d "$_login_json" \
    "$BASE_URL/api/v1/auth/login")
_show_req POST "/api/v1/auth/login" "$_login_sc"
_login_body=$(cat "$_login_file"); rm -f "$_login_file"

if [[ "$_login_sc" -ne 200 ]]; then
    echo -e "  ${RED}Login failed — HTTP $_login_sc.${NC}"
    echo -e "  ${DIM}Server response: $_login_body${NC}"
    echo -e "  ${DIM}Hint: set TINYDM_ADMIN_USER / TINYDM_ADMIN_PASS or restart the server with bootstrap env vars.${NC}"
    exit 1
fi

TOKEN=$(echo "$_login_body" | jfield "['token']" || true)
if [[ -z "$TOKEN" ]]; then
    echo -e "  ${RED}Login returned 200 but no token in response: $_login_body${NC}"; exit 1
fi
AUTH_ARGS=(-H "Authorization: Bearer $TOKEN")
pass "Login succeeded, JWT issued"

# ─────────────────────────────────────────────────────────────────────────────
section "Test Setup"

RUN_ID=$(date +%s)

echo ""
echo -e "  Creating isolated test project and bucket…"
proj=$(_post "/api/v1/tenants/$TENANT_ID/projects" \
    "{\"name\":\"phase5-$RUN_ID\",\"description\":\"Phase 5 audit log tests\"}")
PROJECT_ID=$(echo "$proj" | jfield "['id']")
info "Project: $PROJECT_ID"

bkt=$(_post "/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID/buckets" \
    '{"name":"audit-tests","description":""}')
BUCKET_ID=$(echo "$bkt" | jfield "['id']")
info "Bucket : $BUCKET_ID"

pass "Test project and bucket created"

DOCS="/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID/buckets/$BUCKET_ID/documents"

# ─────────────────────────────────────────────────────────────────────────────
section "5.1 — Mutating operations are recorded"

echo ""
echo -e "  Performing mutations that should each produce an audit event…"

echo -e "\n  [1/4] Upload alpha.txt  →  expect document.create event"
doc_a=$(_post_mp "$DOCS" -F "file=@$WORK/alpha.txt" -F "name=alpha.txt")
DOC_A=$(echo "$doc_a" | jfield "['id']")
info "Document A: $DOC_A"

echo -e "\n  [2/4] Upload beta.txt  →  expect document.create event"
doc_b=$(_post_mp "$DOCS" -F "file=@$WORK/beta.txt" -F "name=beta.txt")
DOC_B=$(echo "$doc_b" | jfield "['id']")
info "Document B: $DOC_B"

echo -e "\n  [3/4] Update Document A  →  expect document.update event"
_put_mp "$DOCS/$DOC_A" -F "file=@$WORK/beta.txt" > /dev/null

echo -e "\n  [4/4] Add tag 'reviewed' to Document B  →  expect document.tag.add event"
_post_tag "$DOCS/$DOC_B/tags/reviewed" > /dev/null

echo ""
echo -e "  Waiting for async audit writes to flush (500 ms)…"
sleep 0.5

echo ""
echo -e "  Querying audit log…"
events=$(audit "?action=document.create")
count=$(echo "$events" | jlen)
assert_ge "$count" "2" \
    "At least 2 document.create events recorded (one per upload)"

events=$(audit "?action=document.*")
wc_count=$(echo "$events" | jlen)
assert_ge "$wc_count" "3" \
    "document.* wildcard matches create + update + tag.add (≥ 3 events)"
assert_contains "$events" '"document.update"' \
    "document.update event present in wildcard results"
assert_contains "$events" '"document.tag.add"' \
    "document.tag.add event present in wildcard results"

# ─────────────────────────────────────────────────────────────────────────────
section "5.2 — Read-only requests (GET) are NOT recorded"

echo ""
echo -e "  Snapshot event count, fire several GETs, confirm count unchanged…"
before_count=$(audit "" | jlen)
info "Event count before GETs: $before_count"

echo ""
echo -e "  Issuing read-only requests…"
_get "$DOCS" > /dev/null
_get "$DOCS/$DOC_A" > /dev/null
_get "$DOCS/$DOC_A/content" > /dev/null
_get "$DOCS/$DOC_B/tags" > /dev/null

echo ""
echo -e "  Waiting 300 ms then re-checking event count…"
sleep 0.3
after_count=$(audit "" | jlen)
info "Event count after GETs:  $after_count"
assert_eq "$after_count" "$before_count" \
    "GET requests do not produce audit events (count unchanged)"

# ─────────────────────────────────────────────────────────────────────────────
section "5.3 — Failed requests (4xx) are NOT recorded"

echo ""
echo -e "  Snapshot event count, fire deliberately bad requests, confirm count unchanged…"
before_count=$(audit "" | jlen)
info "Event count before 4xx tests: $before_count"

echo ""
echo -e "  Issuing requests expected to fail with 4xx…"
_sc "$DOCS/does-not-exist-999" > /dev/null
_sc_delete "$DOCS/does-not-exist-999" > /dev/null

echo ""
echo -e "  Waiting 300 ms then re-checking event count…"
sleep 0.3
after_count=$(audit "" | jlen)
info "Event count after 4xx tests: $after_count"
assert_eq "$after_count" "$before_count" \
    "4xx (failed) requests do not produce audit events (count unchanged)"

# ─────────────────────────────────────────────────────────────────────────────
section "5.4 — Filter by principal"

echo ""
echo -e "  Querying events for principal '$ADMIN_USER'…"
events=$(audit "?principal=$ADMIN_USER")
assert_ge "$(echo "$events" | jlen)" "1" \
    "Events found for principal '$ADMIN_USER'"

principals=$(echo "$events" | python3 -c "
import sys, json
d = json.load(sys.stdin)
events = d['data'] if isinstance(d, dict) and 'data' in d else d
others = [e['principal'] for e in events if e['principal'] != '$ADMIN_USER']
print(','.join(others) if others else 'ok')
")
assert_eq "$principals" "ok" \
    "All returned events belong to principal '$ADMIN_USER'"

echo ""
echo -e "  Querying events for an unknown principal…"
events=$(audit "?principal=no-such-user-xyz")
assert_eq "$(echo "$events" | jlen)" "0" \
    "Unknown principal returns empty data array"

# ─────────────────────────────────────────────────────────────────────────────
section "5.5 — Filter by resource"

echo ""
echo -e "  Querying events for resource=$DOC_A…"
events=$(audit "?resource=$DOC_A")
assert_ge "$(echo "$events" | jlen)" "1" \
    "At least 1 event recorded with Document A as resource"

check=$(echo "$events" | python3 -c "
import sys, json
d = json.load(sys.stdin)
events = d['data'] if isinstance(d, dict) and 'data' in d else d
bad = [e['resource'] for e in events if e['resource'] != '$DOC_A']
print(','.join(bad) if bad else 'ok')
")
assert_eq "$check" "ok" \
    "All returned events reference Document A"

echo ""
echo -e "  Querying events for an unknown resource…"
events=$(audit "?resource=no-such-resource-xyz")
assert_eq "$(echo "$events" | jlen)" "0" \
    "Unknown resource returns empty data array"

# ─────────────────────────────────────────────────────────────────────────────
section "5.6 — Filter by action (exact match)"

echo ""
echo -e "  Querying exact action 'document.create'…"
events=$(audit "?action=document.create")
assert_ge "$(echo "$events" | jlen)" "1" \
    "At least 1 event with exact action 'document.create'"

check=$(echo "$events" | python3 -c "
import sys, json
d = json.load(sys.stdin)
events = d['data'] if isinstance(d, dict) and 'data' in d else d
bad = [e['action'] for e in events if e['action'] != 'document.create']
print(','.join(bad) if bad else 'ok')
")
assert_eq "$check" "ok" \
    "No non-create actions leaked into exact-match results"

echo ""
echo -e "  Querying an action that does not exist…"
events=$(audit "?action=no.such.action")
assert_eq "$(echo "$events" | jlen)" "0" \
    "Unknown action returns empty data array"

# ─────────────────────────────────────────────────────────────────────────────
section "5.7 — Filter by action (wildcard *)"

echo ""
echo -e "  Querying wildcard 'document.*'…"
events=$(audit "?action=document.*")
assert_ge "$(echo "$events" | jlen)" "3" \
    "document.* matches create × 2 + update/tag (≥ 3 events)"

check=$(echo "$events" | python3 -c "
import sys, json
d = json.load(sys.stdin)
events = d['data'] if isinstance(d, dict) and 'data' in d else d
bad = [e['action'] for e in events if not e['action'].startswith('document.')]
print(','.join(bad) if bad else 'ok')
")
assert_eq "$check" "ok" \
    "All events matched by 'document.*' start with 'document.'"

echo ""
echo -e "  Querying broad wildcard '*' (all actions)…"
all_count=$(audit "?action=*" | jlen)
doc_count=$(audit "?action=document.*" | jlen)
assert_ge "$all_count" "$doc_count" \
    "Wildcard '*' returns at least as many events as 'document.*'"

# ─────────────────────────────────────────────────────────────────────────────
section "5.8 — Date-range filters (from / to)"

echo ""
BEFORE_TS=$(date -u +"%Y-%m-%d %H:%M:%S")
echo -e "  Timestamp captured: $BEFORE_TS"
echo -e "  Sleeping 1 s then adding a canary event…"
sleep 1

echo ""
echo -e "  Adding canary tag to Document B…"
_post_tag "$DOCS/$DOC_B/tags/canary" > /dev/null
sleep 0.5

BEFORE_ENC=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$BEFORE_TS'))")

echo ""
echo -e "  Querying events with from=$BEFORE_TS…"
events=$(audit "?from=$BEFORE_ENC")
assert_ge "$(echo "$events" | jlen)" "1" \
    "from= filter returns at least 1 event after the captured timestamp"

echo ""
echo -e "  Querying with to= set to year 2000 (should return nothing from this run)…"
PAST_FROM=$(python3 -c "import urllib.parse; print(urllib.parse.quote('2000-01-01 00:00:00'))")
PAST_TO=$(python3 -c "import urllib.parse; print(urllib.parse.quote('2000-12-31 23:59:59'))")
events=$(audit "?from=$PAST_FROM&to=$PAST_TO")
assert_eq "$(echo "$events" | jlen)" "0" \
    "Date range confined to year 2000 returns no events from this test run"

# ─────────────────────────────────────────────────────────────────────────────
section "5.9 — Pagination (limit / offset)"

echo ""
all_events=$(audit "")
TOTAL=$(echo "$all_events" | jlen)
echo -e "  Total audit events in tenant: ${BOLD}$TOTAL${NC}"

if [[ "$TOTAL" -ge 3 ]]; then
    echo ""
    echo -e "  Fetching page 1 with limit=2…"
    page1=$(audit "?limit=2")
    assert_eq "$(echo "$page1" | jlen)" "2" \
        "limit=2 returns exactly 2 events"

    echo ""
    echo -e "  Fetching page 2 with limit=2&offset=2…"
    page2=$(audit "?limit=2&offset=2")
    assert_ge "$(echo "$page2" | jlen)" "1" \
        "offset=2 returns at least 1 more event"

    id1=$(echo "$page1" | jdata | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])")
    id2=$(echo "$page2" | jdata | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])")
    if [[ "$id1" != "$id2" ]]; then
        pass "Page 1 and page 2 return different events (no overlap)"
    else
        fail "Page 1 and page 2 start with the same event — pagination broken"
    fi
else
    info "Fewer than 3 events — skipping pagination overlap check"
fi

echo ""
echo -e "  Fetching with offset=9999 (beyond total)…"
events=$(audit "?limit=10&offset=9999")
assert_eq "$(echo "$events" | jlen)" "0" \
    "offset beyond total returns empty data array"

echo ""
echo -e "  Fetching with limit=1…"
events=$(audit "?limit=1")
assert_eq "$(echo "$events" | jlen)" "1" \
    "limit=1 returns exactly 1 event"

# ─────────────────────────────────────────────────────────────────────────────
section "5.10 — Access control on the audit endpoint"

echo ""
echo -e "  Testing unauthenticated access (no credentials)…"
sc=$(_sc_noauth "/api/v1/tenants/$TENANT_ID/audit")
assert_eq "$sc" "401" \
    "Unauthenticated request returns 401 Unauthorized"

echo ""
echo -e "  Attempting to create a non-admin user for 403 test…"
RU_RESP=$(curl -sf -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"audittest-$RUN_ID\",\"email\":\"audittest-$RUN_ID@example.com\",\"password\":\"testpass123\",\"is_admin\":false}" \
    "$BASE_URL/api/v1/tenants/$TENANT_ID/users" 2>/dev/null || true)

if echo "$RU_RESP" | grep -q '"id"'; then
    echo -e "  Non-admin user created. Logging in as that user…"
    RU_TOKEN=$(_post "/api/v1/auth/login" \
        "{\"username\":\"audittest-$RUN_ID\",\"password\":\"testpass123\"}" \
        | jfield "['token']" || true)
    if [[ -n "$RU_TOKEN" ]]; then
        echo -e "  Querying audit endpoint as non-admin…"
        sc=$(_sc_bearer "$RU_TOKEN" "/api/v1/tenants/$TENANT_ID/audit")
        assert_eq "$sc" "403" \
            "Non-admin user receives 403 Forbidden on audit endpoint"
    else
        info "Could not log in as non-admin user — skipping 403 test"
    fi
else
    info "User-management endpoint not exposed — 403 test skipped"
    info "(Endpoint is admin-only via auth.RequireAdmin middleware)"
fi

# ─────────────────────────────────────────────────────────────────────────────
section "5.11 — Event shape validation"

echo ""
echo -e "  Fetching one event and checking all required fields are present…"
event=$(audit "?limit=1" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); arr=d['data'] if isinstance(d,dict) and 'data' in d else d; print(json.dumps(arr[0]))" 2>/dev/null || true)
if [[ -n "$event" ]]; then
    info "Sample: $event"
    echo ""
    assert_contains "$event" '"id"'         "Event has 'id' field"
    assert_contains "$event" '"tenant_id"'  "Event has 'tenant_id' field"
    assert_contains "$event" '"principal"'  "Event has 'principal' field"
    assert_contains "$event" '"action"'     "Event has 'action' field"
    assert_contains "$event" '"resource"'   "Event has 'resource' field"
    assert_contains "$event" '"created_at"' "Event has 'created_at' field"
    assert_contains "$event" "\"$TENANT_ID\"" \
        "Event 'tenant_id' matches tenant '$TENANT_ID'"
else
    fail "Could not retrieve a sample event for field validation"
fi

# ─────────────────────────────────────────────────────────────────────────────
section "Cleanup"

echo ""
echo -e "  Deleting test project (cascades to bucket + documents)…"
sc=$(_sc_delete "/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID")
assert_eq "$sc" "204" \
    "Test project deleted (204 No Content)"
info "Audit events are immutable and remain in the log after deletion."

# ─────────────────────────────────────────────────────────────────────────────
TOTAL=$((PASS + FAIL))
echo ""
echo "  ────────────────────────────────────────"
if [[ $FAIL -eq 0 ]]; then
    echo -e "  ${GREEN}${BOLD}All $TOTAL tests passed.${NC}"
else
    echo -e "  ${GREEN}$PASS passed${NC}  ${RED}$FAIL failed${NC}  ($TOTAL total)"
fi
echo "  ────────────────────────────────────────"
echo ""

[[ $FAIL -eq 0 ]]
