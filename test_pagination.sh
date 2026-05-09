#!/usr/bin/env bash
# test_pagination.sh — Integration tests for TinyDM pagination.
#
# Validates the paginated REST API envelope format for all list endpoints:
#   • Response shape: {"data":[...], "pagination":{"total","limit","offset","has_more"}}
#   • limit= and offset= query params respected
#   • has_more is true when more pages exist, false on last page
#   • offset beyond total returns empty data array with correct total
#   • limit clamped to MaxPageLimit (500) when exceeded
#   • Default limit (50) applied when not specified
#   • Pagination works across: tenants, projects, buckets, documents,
#     document versions, users, API keys, and audit events
#
# Prerequisites: curl, python3
# The server must be running before executing this script.
#
# Usage:
#   ./test_pagination.sh [BASE_URL]
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

pass()    { echo -e "  ${GREEN}✓${NC} $1"; PASS=$((PASS + 1)); }
fail()    { echo -e "  ${RED}✗${NC} $1"; FAIL=$((FAIL + 1)); }
section() { echo -e "\n${CYAN}${BOLD}── $1${NC}"; }
info()    { echo -e "  ${DIM}$1${NC}"; }

# ── Assertions ────────────────────────────────────────────────────────────────
assert_eq() {
    local got="$1" want="$2" msg="$3"
    if [[ "$got" == "$want" ]]; then
        pass "$msg"
    else
        fail "$msg"
        echo -e "    ${DIM}want: $want${NC}"
        echo -e "    ${DIM} got: $got${NC}"
    fi
}

assert_ge() {
    local got="$1" want="$2" msg="$3"
    if [[ "$got" -ge "$want" ]]; then
        pass "$msg"
    else
        fail "$msg"
        echo -e "    ${DIM}want: >= $want${NC}"
        echo -e "    ${DIM} got: $got${NC}"
    fi
}

assert_contains() {
    local body="$1" needle="$2" msg="$3"
    if echo "$body" | grep -qF "$needle"; then
        pass "$msg"
    else
        fail "$msg"
        echo -e "    ${DIM}expected to find: $needle${NC}"
        echo -e "    ${DIM}in: ${body:0:400}${NC}"
    fi
}

# ── HTTP helpers ──────────────────────────────────────────────────────────────
AUTH_ARGS=()

# curl -s (no --fail) everywhere: a 4xx/5xx response body is still returned
# so assertions can inspect it, and set -e won't kill the script with exit 22.
_get()     { curl -s ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$BASE_URL$1"; }
_post()    { curl -s -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} -H "Content-Type: application/json" -d "$2" "$BASE_URL$1"; }
_post_mp() { local path=$1; shift; curl -s -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$@" "$BASE_URL$path"; }
_put_mp()  { local path=$1; shift; curl -s -X PUT  ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$@" "$BASE_URL$path"; }
_sc_get()  { curl -s -o /dev/null -w "%{http_code}" ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$BASE_URL$1"; }

# ── JSON helpers (envelope-aware) ─────────────────────────────────────────────
# jfield: extract a field from a single-object JSON response.
jfield() { python3 -c "import sys,json; print(json.load(sys.stdin)$1)" 2>/dev/null || true; }

# jlen: count items in a response — unwraps paginated envelopes automatically.
jlen()  { python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d['data'] if isinstance(d,dict) and 'data' in d else d))" 2>/dev/null || echo "0"; }

# jdata: extract the .data array from a paginated envelope as a JSON array.
jdata() { python3 -c "import sys,json; d=json.load(sys.stdin); import json as j; print(j.dumps(d['data'] if isinstance(d,dict) and 'data' in d else d))" 2>/dev/null || echo "[]"; }

# pag_field: extract a field from .pagination — returns "MISSING" on any error.
pag_field() {
    local resp="$1" field="$2"
    echo "$resp" | python3 -c "import sys,json; d=json.load(sys.stdin); p=d.get('pagination',{}); print(p.get('$field','MISSING'))" 2>/dev/null || echo "MISSING"
}

# assert_envelope: verify a response is a properly shaped pagination envelope.
# Each python3 call falls back to "no" on any error (empty body, bad JSON, etc.)
# so set -e never fires inside this function.
assert_envelope() {
    local resp="$1" label="$2"
    local has_data has_pag has_total has_limit has_offset has_more
    has_data=$(echo "$resp"   | python3 -c "import sys,json; d=json.load(sys.stdin); print('yes' if 'data' in d and isinstance(d['data'],list) else 'no')" 2>/dev/null || echo "no")
    has_pag=$(echo "$resp"    | python3 -c "import sys,json; d=json.load(sys.stdin); print('yes' if 'pagination' in d and isinstance(d['pagination'],dict) else 'no')" 2>/dev/null || echo "no")
    has_total=$(echo "$resp"  | python3 -c "import sys,json; d=json.load(sys.stdin); print('yes' if 'total' in d.get('pagination',{}) else 'no')" 2>/dev/null || echo "no")
    has_limit=$(echo "$resp"  | python3 -c "import sys,json; d=json.load(sys.stdin); print('yes' if 'limit' in d.get('pagination',{}) else 'no')" 2>/dev/null || echo "no")
    has_offset=$(echo "$resp" | python3 -c "import sys,json; d=json.load(sys.stdin); print('yes' if 'offset' in d.get('pagination',{}) else 'no')" 2>/dev/null || echo "no")
    has_more=$(echo "$resp"   | python3 -c "import sys,json; d=json.load(sys.stdin); print('yes' if 'has_more' in d.get('pagination',{}) else 'no')" 2>/dev/null || echo "no")
    assert_eq "$has_data"   "yes" "$label — has data[] array"
    assert_eq "$has_pag"    "yes" "$label — has pagination object"
    assert_eq "$has_total"  "yes" "$label — pagination.total present"
    assert_eq "$has_limit"  "yes" "$label — pagination.limit present"
    assert_eq "$has_offset" "yes" "$label — pagination.offset present"
    assert_eq "$has_more"   "yes" "$label — pagination.has_more present"
}

# ── Prerequisites ─────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}TinyDM — Pagination Integration Tests${NC}"
echo    "Server : $BASE_URL"
echo    "Tenant : $TENANT_ID"

for cmd in curl python3; do
    if ! command -v "$cmd" &>/dev/null; then
        echo -e "${RED}Required tool not found: $cmd${NC}"; exit 1
    fi
done

health=$(curl -sf "$BASE_URL/health" 2>/dev/null || true)
if [[ "$health" != *'"ok"'* ]]; then
    echo -e "${RED}Server not reachable at $BASE_URL — run ./run.sh first.${NC}"; exit 1
fi
echo -e "Health : ${GREEN}ok${NC}"

# ── Temp workspace ────────────────────────────────────────────────────────────
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

echo "hello pagination test" > "$WORK/doc.txt"

# ── Login ─────────────────────────────────────────────────────────────────────
section "Setup — Authenticate"

# Use curl -s (no --fail) so a 4xx response body is still captured and we
# can print a useful diagnostic rather than crashing with exit code 22.
login_resp=$(curl -s -X POST \
    -H "Content-Type: application/json" \
    -d "{\"tenant_id\":\"$TENANT_ID\",\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" \
    "$BASE_URL/api/v1/auth/login")
TOKEN=$(echo "$login_resp" | jfield "['token']" || true)

if [[ -z "$TOKEN" ]]; then
    echo -e "${RED}Login failed. Server response:${NC}"
    echo "  $login_resp"
    exit 1
fi
AUTH_ARGS=(-H "Authorization: Bearer $TOKEN")
pass "Logged in as $ADMIN_USER"

# ── Resolve project/bucket ────────────────────────────────────────────────────
RUN_ID="pag-$(date +%s)"

proj=$(_post "/api/v1/tenants/$TENANT_ID/projects" \
    "{\"name\":\"pag-project-$RUN_ID\",\"description\":\"Pagination test\"}")
PROJECT_ID=$(echo "$proj" | jfield "['id']")
pass "Created project $PROJECT_ID"

bkt=$(_post "/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID/buckets" \
    "{\"name\":\"pag-bucket-$RUN_ID\",\"description\":\"Pagination test\"}")
BUCKET_ID=$(echo "$bkt" | jfield "['id']")
pass "Created bucket $BUCKET_ID"

DOCS="/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID/buckets/$BUCKET_ID/documents"

# ── Upload several documents so we have multiple to page over ─────────────────
section "Setup — Upload test documents"

DOC_IDS=()
for i in 1 2 3 4 5; do
    echo "Document $i content for pagination test run $RUN_ID" > "$WORK/doc$i.txt"
    resp=$(_post_mp "$DOCS" -F "file=@$WORK/doc$i.txt" -F "name=pagtest-$i.txt")
    id=$(echo "$resp" | jfield "['id']")
    DOC_IDS+=("$id")
    info "Uploaded pagtest-$i.txt → $id"
done
pass "Uploaded ${#DOC_IDS[@]} test documents"

# Grab first doc for version tests
DOC_ID="${DOC_IDS[0]}"

# ── Section 1: Envelope shape ─────────────────────────────────────────────────
section "P.1 — Envelope shape on all list endpoints"

resp=$(_get "/api/v1/tenants/$TENANT_ID/projects")
assert_envelope "$resp" "GET /projects"

resp=$(_get "/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID/buckets")
assert_envelope "$resp" "GET /buckets"

resp=$(_get "$DOCS")
assert_envelope "$resp" "GET /documents"

resp=$(_get "/api/v1/tenants/$TENANT_ID/users")
assert_envelope "$resp" "GET /users"

resp=$(_get "/api/v1/tenants/$TENANT_ID/apikeys")
assert_envelope "$resp" "GET /apikeys"

resp=$(_get "/api/v1/tenants/$TENANT_ID/audit")
assert_envelope "$resp" "GET /audit"

# ── Section 2: limit= parameter ───────────────────────────────────────────────
section "P.2 — limit= parameter controls page size"

resp=$(_get "$DOCS?limit=2")
assert_eq "$(echo "$resp" | jlen)" "2" \
    "limit=2 returns exactly 2 documents"
assert_eq "$(pag_field "$resp" "limit")" "2" \
    "pagination.limit echoes back requested limit"
assert_eq "$(pag_field "$resp" "offset")" "0" \
    "pagination.offset is 0 for first page"
assert_eq "$(pag_field "$resp" "has_more")" "True" \
    "pagination.has_more is true when more items remain"

resp=$(_get "$DOCS?limit=1")
assert_eq "$(echo "$resp" | jlen)" "1" \
    "limit=1 returns exactly 1 document"

resp=$(_get "$DOCS?limit=100")
n=$(echo "$resp" | jlen)
assert_ge "$n" "1" \
    "limit=100 returns all available documents (≥ 1)"
assert_eq "$(pag_field "$resp" "has_more")" "False" \
    "pagination.has_more is false when all items fit on one page"

# ── Section 3: offset= parameter ─────────────────────────────────────────────
section "P.3 — offset= parameter skips items"

page1=$(_get "$DOCS?limit=2&offset=0")
page2=$(_get "$DOCS?limit=2&offset=2")

n1=$(echo "$page1" | jlen)
n2=$(echo "$page2" | jlen)
assert_ge "$n1" "1" "Page 1 (limit=2, offset=0) has items"
assert_ge "$n2" "1" "Page 2 (limit=2, offset=2) has items"

# IDs on page 1 and page 2 must not overlap
id1_0=$(echo "$page1" | jdata | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])")
id2_0=$(echo "$page2" | jdata | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])")
if [[ "$id1_0" != "$id2_0" ]]; then
    pass "Page 1 and page 2 first items are different (no overlap)"
else
    fail "Page 1 and page 2 both start with the same item — offset broken"
fi

# ── Section 4: pagination.total ───────────────────────────────────────────────
section "P.4 — pagination.total reflects the full count"

full=$(_get "$DOCS?limit=100")
total=$(pag_field "$full" "total")
assert_ge "$total" "5" \
    "pagination.total >= 5 (we uploaded 5 docs in this run)"

# Requesting with limit=1 should report the same total
one=$(_get "$DOCS?limit=1")
total_one=$(pag_field "$one" "total")
assert_eq "$total_one" "$total" \
    "pagination.total is the same regardless of limit"

# ── Section 5: offset beyond total ───────────────────────────────────────────
section "P.5 — offset beyond total returns empty data[]"

resp=$(_get "$DOCS?limit=10&offset=999999")
assert_eq "$(echo "$resp" | jlen)" "0" \
    "offset=999999 returns 0 documents in data[]"
assert_eq "$(pag_field "$resp" "has_more")" "False" \
    "has_more is false when offset is beyond total"
total_oob=$(pag_field "$resp" "total")
assert_ge "$total_oob" "5" \
    "pagination.total still reflects actual count even at out-of-bounds offset"

# ── Section 6: has_more flag ──────────────────────────────────────────────────
section "P.6 — has_more flag semantics"

# Page that definitely has more
resp=$(_get "$DOCS?limit=1&offset=0")
assert_eq "$(pag_field "$resp" "has_more")" "True" \
    "has_more=true when offset+limit < total"

# Last-item fetch: offset = total-1, limit = 1
total=$(pag_field "$(_get "$DOCS?limit=100")" "total")
last_offset=$(( total - 1 ))
resp=$(_get "$DOCS?limit=1&offset=$last_offset")
assert_eq "$(echo "$resp" | jlen)" "1" \
    "Fetching the last item returns exactly 1 document"
assert_eq "$(pag_field "$resp" "has_more")" "False" \
    "has_more=false when on the final item"

# ── Section 7: document versions pagination ───────────────────────────────────
section "P.7 — Document versions endpoint is paginated"

# Create a second version for DOC_ID
echo "version 2 content" > "$WORK/v2.txt"
_put_mp "$DOCS/$DOC_ID" -F "file=@$WORK/v2.txt" > /dev/null

resp=$(_get "$DOCS/$DOC_ID/versions")
assert_envelope "$resp" "GET /versions"
assert_ge "$(echo "$resp" | jlen)" "1" \
    "At least 1 version snapshot in history"

resp2=$(_get "$DOCS/$DOC_ID/versions?limit=1")
assert_eq "$(echo "$resp2" | jlen)" "1" \
    "limit=1 on versions returns exactly 1 snapshot"

# ── Section 8: audit endpoint pagination ─────────────────────────────────────
section "P.8 — Audit endpoint pagination"

# Ensure there are audit events from our setup work
sleep 0.3
all_audit=$(_get "/api/v1/tenants/$TENANT_ID/audit")
audit_total=$(pag_field "$all_audit" "total")
info "Total audit events in tenant: $audit_total"
assert_ge "$audit_total" "1" "Audit log has at least 1 event"

resp=$(_get "/api/v1/tenants/$TENANT_ID/audit?limit=1")
assert_eq "$(echo "$resp" | jlen)" "1" \
    "limit=1 on audit returns exactly 1 event"
assert_eq "$(pag_field "$resp" "limit")" "1" \
    "audit pagination.limit echoes 1"

if [[ "$audit_total" -ge 2 ]]; then
    resp_p2=$(_get "/api/v1/tenants/$TENANT_ID/audit?limit=1&offset=1")
    assert_ge "$(echo "$resp_p2" | jlen)" "1" \
        "audit offset=1 returns at least 1 event"
    # IDs must differ
    eid1=$(echo "$resp"    | jdata | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])")
    eid2=$(echo "$resp_p2" | jdata | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])")
    if [[ "$eid1" != "$eid2" ]]; then
        pass "Audit page 1 and page 2 return different events"
    else
        fail "Audit page 1 and page 2 start with the same event — offset broken"
    fi
fi

resp_oob=$(_get "/api/v1/tenants/$TENANT_ID/audit?limit=10&offset=999999")
assert_eq "$(echo "$resp_oob" | jlen)" "0" \
    "audit offset=999999 returns 0 events"

# ── Section 9: users and API keys ─────────────────────────────────────────────
section "P.9 — Users and API keys pagination"

resp=$(_get "/api/v1/tenants/$TENANT_ID/users?limit=1")
assert_eq "$(echo "$resp" | jlen)" "1" \
    "limit=1 on users returns exactly 1 user"
assert_eq "$(pag_field "$resp" "limit")" "1" \
    "users pagination.limit echoes 1"

resp=$(_get "/api/v1/tenants/$TENANT_ID/apikeys?limit=1&offset=999")
assert_eq "$(echo "$resp" | jlen)" "0" \
    "apikeys offset=999 returns empty data[]"
assert_eq "$(pag_field "$resp" "has_more")" "False" \
    "apikeys has_more=false at out-of-bounds offset"

# ── Section 10: default limit ─────────────────────────────────────────────────
section "P.10 — Default limit is 50"

resp=$(_get "$DOCS")
default_limit=$(pag_field "$resp" "limit")
assert_eq "$default_limit" "50" \
    "Default limit is 50 when ?limit= is omitted"

resp=$(_get "/api/v1/tenants/$TENANT_ID/audit")
audit_default_limit=$(pag_field "$resp" "limit")
assert_eq "$audit_default_limit" "50" \
    "Audit default limit is 50 when ?limit= is omitted"

# ── Section 11: search and tag-filter also paginated ─────────────────────────
section "P.11 — Search (?q=) and tag filter (?tag=) return paginated envelopes"

# Search by partial name
resp=$(_get "$DOCS?q=pagtest&limit=2")
assert_envelope "$resp" "GET /documents?q=pagtest"
assert_ge "$(echo "$resp" | jlen)" "1" \
    "?q=pagtest finds at least one document"
assert_eq "$(pag_field "$resp" "limit")" "2" \
    "?q= search respects limit=2"

# Tag filter with known-empty tag
resp=$(_get "$DOCS?tag=no-such-tag-pagination-test")
assert_envelope "$resp" "GET /documents?tag=<missing>"
assert_eq "$(echo "$resp" | jlen)" "0" \
    "?tag=<unknown> returns empty data[] (not bare [])"
assert_eq "$(pag_field "$resp" "has_more")" "False" \
    "?tag=<unknown> has_more=false"

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "──────────────────────────────────────────────────"
TOTAL=$((PASS + FAIL))
if [[ "$FAIL" -eq 0 ]]; then
    echo -e "${GREEN}${BOLD}All $TOTAL pagination tests passed.${NC}"
else
    echo -e "${RED}${BOLD}$FAIL / $TOTAL tests FAILED.${NC}"
fi
echo "──────────────────────────────────────────────────"
echo ""

[[ "$FAIL" -eq 0 ]]
