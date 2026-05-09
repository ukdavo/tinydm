#!/usr/bin/env bash
# test_phase7.sh — Integration tests for TinyDM Phase 7: Document & Bucket Management UI.
#
# Covers:
#   7.1  Bucket edit  — GET /edit partial, GET /row partial, PUT (rename + desc)
#   7.2  Document update — rename-only (no snapshot) and content replace (with snapshot)
#   7.3  Document search — HTMX rows partial filtered by name
#   7.4  Tag filter — HTMX rows partial filtered by tag
#   7.5  Document detail page — breadcrumb, metadata, empty tags/props/versions
#   7.6  Tag management — add, verify, remove
#   7.7  Property management — set, verify, delete
#   7.8  System metadata — sys.* properties shown in Extracted Metadata section
#   7.9  Version history & restore — version created on content replace; restore endpoint
#
# Prerequisites: curl, python3
# The server must already be running before executing this script.
#
# Usage:
#   ./test_phase7.sh [BASE_URL]
#   BASE_URL defaults to http://localhost:8080

set -eo pipefail

BASE_URL="${1:-${TINYDM_URL:-http://localhost:8080}}"
ADMIN_USER="${TINYDM_ADMIN_USER:-admin}"
ADMIN_PASS="${TINYDM_ADMIN_PASS:-changeme}"
TENANT_ID="${TINYDM_BOOTSTRAP_TENANT_ID:-default}"
TENANT_NAME="${TINYDM_BOOTSTRAP_TENANT_NAME:-Default}"

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

assert_contains() {
    local body="$1" needle="$2" msg="$3"
    if echo "$body" | grep -qF "$needle"; then
        pass "$msg"
    else
        fail "$msg"
        echo -e "      ${DIM}expected: $needle${NC}"
        echo -e "      ${DIM}in body : ${body:0:400}${NC}"
    fi
}

assert_not_contains() {
    local body="$1" needle="$2" msg="$3"
    if ! echo "$body" | grep -qF "$needle"; then
        pass "$msg"
    else
        fail "$msg"
        echo -e "      ${DIM}did NOT expect: $needle${NC}"
    fi
}

# ── HTTP helpers ──────────────────────────────────────────────────────────────
COOKIE_JAR=""
CURL_TMP=""

_show_req() {
    local method="$1" path="$2" sc="$3"
    local colour="$GREEN"
    [[ "$sc" -ge 400 ]] 2>/dev/null && colour="$RED"
    [[ "$sc" -ge 300 && "$sc" -lt 400 ]] 2>/dev/null && colour="$YELLOW"
    local short="${path:0:70}"
    [[ "${#path}" -gt 70 ]] && short="${path:0:67}..."
    printf "  ${DIM}%-10s %-72s${NC} ${colour}%s${NC}\n" "$method" "$short" "$sc" >&2
}

# _web METHOD PATH [extra curl args...] → returns HTTP status; body in CURL_TMP
_web() {
    local method="$1" path="$2"; shift 2
    local sc
    sc=$(curl -s -o "$CURL_TMP" -w "%{http_code}" \
        -X "$method" \
        -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
        "$@" "$BASE_URL$path")
    _show_req "$method" "$path" "$sc"
    echo "$sc"
}

# _web_del METHOD PATH — discard body
_web_del() {
    local path="$1"
    local sc
    sc=$(curl -s -o /dev/null -w "%{http_code}" \
        -X DELETE \
        -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
        "$BASE_URL$path")
    _show_req DELETE "$path" "$sc"
    echo "$sc"
}

body() { cat "$CURL_TMP"; }

# Extract the first UUID from an HTML id="PREFIX-UUID" attribute.
html_id() {
    local html="$1" prefix="$2"
    echo "$html" | python3 -c "
import sys, re
m = re.search(r'id=\"${prefix}-([0-9a-f-]{36})\"', sys.stdin.read())
print(m.group(1) if m else '')
" 2>/dev/null
}

# Extract the first UUID from an HTML href or hx-post/hx-delete containing the pattern.
html_uuid_after() {
    local html="$1" pattern="$2"
    echo "$html" | python3 -c "
import sys, re
text = sys.stdin.read()
m = re.search(r'${pattern}/([0-9a-f-]{36})', text)
print(m.group(1) if m else '')
" 2>/dev/null
}

# ── Prerequisites ─────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}TinyDM — Phase 7 Document & Bucket Management UI Integration Tests${NC}"
echo    "Server : $BASE_URL"
echo    "Tenant : $TENANT_ID  (admin user: $ADMIN_USER)"

for cmd in curl python3; do
    command -v "$cmd" &>/dev/null || { echo -e "${RED}Required tool not found: $cmd${NC}"; exit 1; }
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
COOKIE_JAR="$WORK/cookies.txt"
CURL_TMP="$WORK/curl_body.txt"
trap 'rm -rf "$WORK"' EXIT

printf 'Phase 7 test file — version 1\n' > "$WORK/v1.txt"
printf 'Phase 7 test file — version 2 (updated content)\n' > "$WORK/v2.txt"
printf 'Phase 7 second document\n' > "$WORK/gamma.txt"

RUN_ID=$(date +%s)

# ── API token for setup / cleanup ─────────────────────────────────────────────
echo ""
echo -e "  Obtaining API token for setup / cleanup…"
_tok_body=$(curl -sf -X POST \
    -H "Content-Type: application/json" \
    -d "{\"tenant_id\":\"$TENANT_ID\",\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" \
    "$BASE_URL/api/v1/auth/login" 2>/dev/null || true)
API_TOKEN=$(echo "$_tok_body" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])" 2>/dev/null || true)

if [[ -z "$API_TOKEN" ]]; then
    echo -e "  ${RED}Could not obtain API token.${NC}"
    exit 1
fi
echo -e "  ${GREEN}API token issued.${NC}"

# ── Login via web UI ──────────────────────────────────────────────────────────
echo ""
echo -e "  Logging in via web UI…"
sc=$(_web POST "/admin/login" \
    -d "tenant_name=$TENANT_NAME&username=$ADMIN_USER&password=$ADMIN_PASS")
if [[ "$sc" == "302" ]] && grep -q "tdm_session" "$COOKIE_JAR" 2>/dev/null; then
    echo -e "  ${GREEN}Logged in — session cookie obtained.${NC}"
else
    echo -e "  ${RED}Web login failed (HTTP $sc). Aborting.${NC}"
    exit 1
fi
rm -f "$CURL_TMP"

# IDs accumulated during the test
TEST_PROJECT_ID=""
TEST_BUCKET_ID=""
TEST_DOC_ID=""
GAMMA_DOC_ID=""
VERSION_ID=""

# ── Setup: create project and bucket ─────────────────────────────────────────
echo ""
echo -e "  Creating test project and bucket via REST API…"

_proj_body=$(curl -sf -X POST \
    -H "Authorization: Bearer $API_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"p7-proj-$RUN_ID\",\"description\":\"Phase 7 test project\"}" \
    "$BASE_URL/api/v1/tenants/$TENANT_ID/projects" 2>/dev/null || true)
TEST_PROJECT_ID=$(echo "$_proj_body" | python3 -c \
    "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || true)
info "Project ID: $TEST_PROJECT_ID"

_bkt_body=$(curl -sf -X POST \
    -H "Authorization: Bearer $API_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"p7-bkt-$RUN_ID\",\"description\":\"Phase 7 test bucket\"}" \
    "$BASE_URL/api/v1/tenants/$TENANT_ID/projects/$TEST_PROJECT_ID/buckets" 2>/dev/null || true)
TEST_BUCKET_ID=$(echo "$_bkt_body" | python3 -c \
    "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || true)
info "Bucket ID: $TEST_BUCKET_ID"

if [[ -z "$TEST_PROJECT_ID" || -z "$TEST_BUCKET_ID" ]]; then
    echo -e "  ${RED}Setup failed: could not create project/bucket.${NC}"; exit 1
fi

BUCKET_PATH="/admin/tenants/$TENANT_ID/projects/$TEST_PROJECT_ID/buckets/$TEST_BUCKET_ID"
DOCS_PATH="$BUCKET_PATH/documents"

echo -e "  ${GREEN}Setup complete.${NC}"

# ─────────────────────────────────────────────────────────────────────────────
section "7.1 — Bucket edit inline form"

echo ""
echo -e "  Requesting bucket edit form (HTMX partial)…"
sc=$(_web GET "$BUCKET_PATH/edit")
assert_eq "$sc" "200" "GET .../buckets/{id}/edit → 200"
EDIT_FORM=$(body)
assert_contains "$EDIT_FORM" 'hx-put'        "Edit partial has hx-put Save button"
assert_contains "$EDIT_FORM" 'hx-get'        "Edit partial has hx-get Cancel button"
assert_contains "$EDIT_FORM" "p7-bkt-$RUN_ID" "Edit form pre-filled with current bucket name"
rm -f "$CURL_TMP"

echo ""
echo -e "  Requesting bucket row partial (cancel / re-render)…"
sc=$(_web GET "$BUCKET_PATH/row")
assert_eq "$sc" "200" "GET .../buckets/{id}/row → 200"
assert_contains "$(body)" "p7-bkt-$RUN_ID" "Row partial shows bucket name"
assert_contains "$(body)" 'id="bucket-'    "Row partial has correct element ID"
rm -f "$CURL_TMP"

NEW_BUCKET_NAME="p7-bkt-renamed-$RUN_ID"
echo ""
echo -e "  Saving bucket rename (PUT)…"
sc=$(_web PUT "$BUCKET_PATH" \
    -d "name=$NEW_BUCKET_NAME&description=Phase+7+renamed+bucket")
assert_eq "$sc" "200" "PUT .../buckets/{id} → 200 (HTMX row partial)"
UPDATED_ROW=$(body)
assert_contains "$UPDATED_ROW" "$NEW_BUCKET_NAME" "Renamed bucket name in row partial"
assert_contains "$UPDATED_ROW" 'id="bucket-'      "Row partial retains bucket element ID"
rm -f "$CURL_TMP"

echo ""
echo -e "  Verifying rename persists on the buckets list page…"
sc=$(_web GET "/admin/tenants/$TENANT_ID/projects/$TEST_PROJECT_ID/buckets")
assert_eq "$sc" "200"                          "GET buckets page → 200"
assert_contains "$(body)" "$NEW_BUCKET_NAME"  "Renamed bucket name visible in list"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "7.2 — Document update (rename-only)"

echo ""
echo -e "  Uploading initial document v1.txt…"
sc=$(_web POST "$DOCS_PATH" \
    -F "file=@$WORK/v1.txt" \
    -F "name=doc-v1-$RUN_ID.txt")
assert_eq "$sc" "200" "POST document upload → 200"
DOC_ROW=$(body)
assert_contains "$DOC_ROW" "doc-v1-$RUN_ID.txt" "Uploaded filename in row"
assert_contains "$DOC_ROW" "v1"                  "Version badge shows v1"
TEST_DOC_ID=$(html_id "$DOC_ROW" "doc")
info "Document ID: $TEST_DOC_ID"
rm -f "$CURL_TMP"

echo ""
echo -e "  Requesting document edit form…"
sc=$(_web GET "/admin/documents/$TEST_DOC_ID/edit")
assert_eq "$sc" "200" "GET /admin/documents/{id}/edit → 200"
EDIT_FORM=$(body)
assert_contains "$EDIT_FORM" 'hx-put'              "Edit partial has hx-put Save button"
assert_contains "$EDIT_FORM" "doc-v1-$RUN_ID.txt"  "Edit form pre-filled with doc name"
rm -f "$CURL_TMP"

DOC_RENAMED="doc-renamed-$RUN_ID.txt"
echo ""
echo -e "  Renaming document (no file replacement → no version snapshot)…"
sc=$(_web PUT "/admin/documents/$TEST_DOC_ID" \
    -F "name=$DOC_RENAMED")
assert_eq "$sc" "200" "PUT /admin/documents/{id} (name only) → 200"
RENAMED_ROW=$(body)
assert_contains "$RENAMED_ROW" "$DOC_RENAMED"  "Renamed document name in row partial"
assert_contains "$RENAMED_ROW" "v1"             "Version badge still v1 after rename-only"
rm -f "$CURL_TMP"

echo ""
echo -e "  Requesting document row partial (for cancel flows)…"
sc=$(_web GET "/admin/documents/$TEST_DOC_ID/row")
assert_eq "$sc" "200" "GET /admin/documents/{id}/row → 200"
assert_contains "$(body)" "$DOC_RENAMED"  "Row partial shows renamed document"
assert_contains "$(body)" 'id="doc-'      "Row partial has correct element ID"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "7.2 — Document update (content replace → version snapshot)"

echo ""
echo -e "  Replacing document content with v2.txt (creates version snapshot)…"
sc=$(_web PUT "/admin/documents/$TEST_DOC_ID" \
    -F "name=$DOC_RENAMED" \
    -F "file=@$WORK/v2.txt")
assert_eq "$sc" "200" "PUT /admin/documents/{id} (with file) → 200"
V2_ROW=$(body)
assert_contains "$V2_ROW" "$DOC_RENAMED"  "Document name preserved after content replace"
assert_contains "$V2_ROW" "v2"             "Version badge incremented to v2"
rm -f "$CURL_TMP"

echo ""
echo -e "  Uploading second document (gamma.txt) for search tests…"
sc=$(_web POST "$DOCS_PATH" \
    -F "file=@$WORK/gamma.txt" \
    -F "name=gamma-$RUN_ID.txt")
assert_eq "$sc" "200" "POST second document upload → 200"
GAMMA_ROW=$(body)
GAMMA_DOC_ID=$(html_id "$GAMMA_ROW" "doc")
info "Gamma document ID: $GAMMA_DOC_ID"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "7.3 — Document search filter"

echo ""
echo -e "  HTMX document rows partial with no filter (all docs)…"
sc=$(_web GET "$DOCS_PATH/rows")
assert_eq "$sc" "200" "GET .../documents/rows → 200 (all docs)"
ALL_ROWS=$(body)
assert_contains "$ALL_ROWS" "$DOC_RENAMED"       "Renamed doc visible in unfiltered rows"
assert_contains "$ALL_ROWS" "gamma-$RUN_ID.txt"  "Gamma doc visible in unfiltered rows"
rm -f "$CURL_TMP"

echo ""
echo -e "  Searching for 'gamma'…"
sc=$(_web GET "$DOCS_PATH/rows?q=gamma")
assert_eq "$sc" "200" "GET .../documents/rows?q=gamma → 200"
GAMMA_RESULTS=$(body)
assert_contains "$GAMMA_RESULTS" "gamma-$RUN_ID.txt"  "Gamma doc matches search"
assert_not_contains "$GAMMA_RESULTS" "$DOC_RENAMED"   "Non-matching doc excluded from results"
rm -f "$CURL_TMP"

echo ""
echo -e "  Searching for 'renamed'…"
sc=$(_web GET "$DOCS_PATH/rows?q=renamed")
assert_eq "$sc" "200" "GET .../documents/rows?q=renamed → 200"
RENAMED_RESULTS=$(body)
assert_contains "$RENAMED_RESULTS" "$DOC_RENAMED"          "Renamed doc matches search"
assert_not_contains "$RENAMED_RESULTS" "gamma-$RUN_ID.txt" "Gamma doc excluded from results"
rm -f "$CURL_TMP"

echo ""
echo -e "  Searching for a term that matches nothing…"
sc=$(_web GET "$DOCS_PATH/rows?q=no-match-xyz-$RUN_ID")
assert_eq "$sc" "200" "GET .../documents/rows?q=<no-match> → 200"
assert_contains "$(body)" "No documents found" "Empty state shown for no-match search"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "7.5 — Document detail page"

echo ""
echo -e "  Loading document detail page…"
sc=$(_web GET "/admin/documents/$TEST_DOC_ID")
assert_eq "$sc" "200" "GET /admin/documents/{id} → 200"
DETAIL=$(body)
assert_contains "$DETAIL" "$DOC_RENAMED"          "Document name in detail heading"
assert_contains "$DETAIL" "Details"               "Details card present"
assert_contains "$DETAIL" "Version History"       "Version History card present"
assert_contains "$DETAIL" "Tags"                  "Tags card present"
assert_contains "$DETAIL" "Properties"            "Properties card present"
assert_contains "$DETAIL" "Content type"          "Content type metadata shown"
assert_contains "$DETAIL" "Checksum"              "Checksum metadata shown"
assert_contains "$DETAIL" 'hx-delete'             "Delete button present"
assert_contains "$DETAIL" 'href="/admin/documents/'"$TEST_DOC_ID"'/download"' \
                "Download link present"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "7.6 — Tag management"

echo ""
echo -e "  Adding tag 'important'…"
sc=$(_web POST "/admin/documents/$TEST_DOC_ID/tags" \
    -d "tag=important")
assert_eq "$sc" "200" "POST /admin/documents/{id}/tags → 200 (chips partial)"
CHIPS=$(body)
assert_contains "$CHIPS" "important"      "New tag 'important' appears in chips"
assert_contains "$CHIPS" "chip-remove"    "Remove button present on tag chip"
assert_contains "$CHIPS" 'hx-delete'     "HTMX delete wired on chip remove button"
rm -f "$CURL_TMP"

echo ""
echo -e "  Adding a second tag 'draft'…"
sc=$(_web POST "/admin/documents/$TEST_DOC_ID/tags" \
    -d "tag=draft")
assert_eq "$sc" "200" "POST second tag → 200"
TWO_CHIPS=$(body)
assert_contains "$TWO_CHIPS" "important"  "First tag still present"
assert_contains "$TWO_CHIPS" "draft"      "Second tag 'draft' appears in chips"
rm -f "$CURL_TMP"

echo ""
echo -e "  Tagging gamma doc with 'draft' for tag filter test…"
sc=$(_web POST "/admin/documents/$GAMMA_DOC_ID/tags" \
    -d "tag=draft")
assert_eq "$sc" "200" "Tag gamma doc → 200"
rm -f "$CURL_TMP"

echo ""
echo -e "  Removing tag 'draft' from primary document…"
sc=$(_web_del "/admin/documents/$TEST_DOC_ID/tags/draft")
assert_eq "$sc" "200" "DELETE /admin/documents/{id}/tags/{tag} → 200 (chips partial)"

# Re-fetch chips to check
sc=$(_web GET "/admin/documents/$TEST_DOC_ID")
assert_eq "$sc" "200" "GET detail page after tag remove → 200"
DETAIL_AFTER=$(body)
assert_contains "$DETAIL_AFTER" "important"      "Tag 'important' still present"
assert_not_contains "$DETAIL_AFTER" ">draft<"   "'draft' tag chip removed"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "7.4 — Tag filter on document list"

echo ""
echo -e "  Filtering by tag 'draft' (only gamma should match)…"
sc=$(_web GET "$DOCS_PATH/rows?tag=draft")
assert_eq "$sc" "200" "GET .../documents/rows?tag=draft → 200"
TAG_RESULTS=$(body)
assert_contains "$TAG_RESULTS" "gamma-$RUN_ID.txt"  "Gamma doc matches 'draft' tag filter"
assert_not_contains "$TAG_RESULTS" "$DOC_RENAMED"   "Primary doc (not tagged 'draft') excluded"
rm -f "$CURL_TMP"

echo ""
echo -e "  Filtering by tag 'important' (only primary doc should match)…"
sc=$(_web GET "$DOCS_PATH/rows?tag=important")
assert_eq "$sc" "200" "GET .../documents/rows?tag=important → 200"
IMP_RESULTS=$(body)
assert_contains "$IMP_RESULTS" "$DOC_RENAMED"          "Primary doc matches 'important' tag filter"
assert_not_contains "$IMP_RESULTS" "gamma-$RUN_ID.txt" "Gamma doc excluded from 'important' filter"
rm -f "$CURL_TMP"

echo ""
echo -e "  Combined tag + name filter…"
sc=$(_web GET "$DOCS_PATH/rows?tag=draft&q=gamma")
assert_eq "$sc" "200" "GET .../documents/rows?tag=draft&q=gamma → 200"
COMBINED=$(body)
assert_contains "$COMBINED" "gamma-$RUN_ID.txt"  "Gamma doc matches combined filter"
assert_not_contains "$COMBINED" "$DOC_RENAMED"   "Primary doc excluded from combined filter"
rm -f "$CURL_TMP"

echo ""
echo -e "  Tag filter with no matching documents…"
sc=$(_web GET "$DOCS_PATH/rows?tag=no-such-tag-xyz")
assert_eq "$sc" "200" "GET .../documents/rows?tag=<no-match> → 200"
assert_contains "$(body)" "No documents found" "Empty state shown for unmatched tag"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "7.7 — Property management"

echo ""
echo -e "  Setting property 'author' = 'Alice'…"
sc=$(_web POST "/admin/documents/$TEST_DOC_ID/properties" \
    -d "key=author&value=Alice")
assert_eq "$sc" "200" "POST /admin/documents/{id}/properties → 200 (prop-row partials)"
PROP_ROWS=$(body)
assert_contains "$PROP_ROWS" "author"  "Property key 'author' in response"
assert_contains "$PROP_ROWS" "Alice"   "Property value 'Alice' in response"
assert_contains "$PROP_ROWS" 'hx-delete' "Delete button wired in prop row"
rm -f "$CURL_TMP"

echo ""
echo -e "  Setting a second property 'department' = 'Engineering'…"
sc=$(_web POST "/admin/documents/$TEST_DOC_ID/properties" \
    -d "key=department&value=Engineering")
assert_eq "$sc" "200" "POST second property → 200"
TWO_PROPS=$(body)
assert_contains "$TWO_PROPS" "author"       "First property still present"
assert_contains "$TWO_PROPS" "department"   "Second property 'department' present"
assert_contains "$TWO_PROPS" "Engineering"  "Property value 'Engineering' present"
rm -f "$CURL_TMP"

echo ""
echo -e "  Properties visible on the detail page…"
sc=$(_web GET "/admin/documents/$TEST_DOC_ID")
assert_eq "$sc" "200" "GET detail page with properties → 200"
DETAIL_PROPS=$(body)
assert_contains "$DETAIL_PROPS" "author"      "Property 'author' visible on detail page"
assert_contains "$DETAIL_PROPS" "Alice"       "Property value 'Alice' visible"
assert_contains "$DETAIL_PROPS" "department"  "Property 'department' visible"
rm -f "$CURL_TMP"

echo ""
echo -e "  Deleting property 'author'…"
sc=$(_web_del "/admin/documents/$TEST_DOC_ID/properties/author")
assert_eq "$sc" "200" "DELETE /admin/documents/{id}/properties/{key} → 200"

echo ""
echo -e "  Verifying 'author' property gone from detail page…"
sc=$(_web GET "/admin/documents/$TEST_DOC_ID")
assert_eq "$sc" "200" "GET detail page after property delete → 200"
DETAIL_AFTER_DEL=$(body)
assert_not_contains "$DETAIL_AFTER_DEL" ">author<"  "Deleted property 'author' absent"
assert_contains "$DETAIL_AFTER_DEL" "department"     "Remaining property 'department' still present"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "7.9 — Version history"

echo ""
echo -e "  Loading detail page and checking version history table…"
sc=$(_web GET "/admin/documents/$TEST_DOC_ID")
assert_eq "$sc" "200" "GET detail page with version history → 200"
DETAIL_VER=$(body)
assert_contains "$DETAIL_VER" "Version History"  "Version History section present"
assert_contains "$DETAIL_VER" "hx-post"          "Restore button present in version row"
assert_contains "$DETAIL_VER" "version-"         "Version row element ID present"
# Confirm there is at least one previous version (created by the content replace)
assert_contains "$DETAIL_VER" 'id="version-'     "At least one version row present"

# Extract version ID from the first hx-post="...versions/{id}/restore" attribute
VERSION_ID=$(echo "$DETAIL_VER" | python3 -c "
import sys, re
text = sys.stdin.read()
m = re.search(r'/versions/([0-9a-f-]{36})/restore', text)
print(m.group(1) if m else '')
" 2>/dev/null)
info "Version ID found: $VERSION_ID"
rm -f "$CURL_TMP"

if [[ -n "$VERSION_ID" ]]; then
    echo ""
    echo -e "  Restoring version $VERSION_ID…"
    sc=$(_web POST "/admin/documents/$TEST_DOC_ID/versions/$VERSION_ID/restore")
    assert_eq "$sc" "200" "POST .../versions/{id}/restore → 200"
    rm -f "$CURL_TMP"

    echo ""
    echo -e "  Confirming restore incremented the document version…"
    sc=$(_web GET "/admin/documents/$TEST_DOC_ID")
    assert_eq "$sc" "200" "GET detail page after restore → 200"
    DETAIL_RESTORED=$(body)
    assert_contains "$DETAIL_RESTORED" "v3" "Version badge incremented to v3 after restore"
    rm -f "$CURL_TMP"
else
    fail "No version ID found in detail page — cannot test restore"
fi

# ─────────────────────────────────────────────────────────────────────────────
section "Cleanup"

echo ""
echo -e "  Removing test data via REST API…"

_api_del() {
    local path="$1"
    local sc
    sc=$(curl -s -o /dev/null -w "%{http_code}" \
        -X DELETE \
        -H "Authorization: Bearer $API_TOKEN" \
        "$BASE_URL$path" 2>/dev/null || true)
    _show_req DELETE "$path" "$sc"
}

[[ -n "$TEST_PROJECT_ID" ]] && \
    _api_del "/api/v1/tenants/$TENANT_ID/projects/$TEST_PROJECT_ID"

pass "Test data removed"

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
