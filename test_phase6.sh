#!/usr/bin/env bash
# test_phase6.sh — Integration tests for TinyDM Phase 6: Admin Web UI.
#
# Covers:
#   6.1  Session middleware — unauthenticated requests redirect to /admin/login
#   6.2  Embedded static assets served at /admin/static/
#   6.3  Login page — renders correctly; rejects bad credentials; issues session cookie
#   6.4  Dashboard — authenticated access; stat cards and recent-activity table
#   6.5  Tenant / project / bucket browser — HTMX create, page render, delete
#   6.6  Document management — multipart upload, download, delete
#   6.7  User management — create, activate/deactivate, delete
#   6.8  API key management — generate (plaintext shown once), revoke
#   6.9  Audit log viewer — page, HTMX events partial, action filter, empty state
#        (also verifies that logout clears the session)
#
# Prerequisites: curl, python3
# The server must already be running before executing this script.
#
# Usage:
#   ./test_phase6.sh [BASE_URL]
#   BASE_URL defaults to http://localhost:8080

set -eo pipefail

BASE_URL="${1:-${TINYDM_URL:-http://localhost:8080}}"
ADMIN_USER="${TINYDM_ADMIN_USER:-admin}"
ADMIN_PASS="${TINYDM_ADMIN_PASS:-changeme}"
TENANT_ID="${TINYDM_BOOTSTRAP_TENANT_ID:-default}"

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
# All web-UI requests go through COOKIE_JAR so the session is maintained across
# calls. CURL_TMP holds the response body from the last _web call.
COOKIE_JAR=""
CURL_TMP=""

_show_req() {
    local method="$1" path="$2" sc="$3"
    local colour="$GREEN"
    [[ "$sc" -ge 400 ]] 2>/dev/null && colour="$RED"
    [[ "$sc" -ge 300 && "$sc" -lt 400 ]] 2>/dev/null && colour="$YELLOW"
    local short="${path:0:60}"
    [[ "${#path}" -gt 60 ]] && short="${path:0:57}..."
    printf "  ${DIM}%-10s %-62s${NC} ${colour}%s${NC}\n" "$method" "$short" "$sc" >&2
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

# Status-only request without any cookie (tests unauthenticated behaviour)
_noauth() {
    local path="$1"
    local sc
    sc=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL$path")
    _show_req "GET(nc)" "$path" "$sc"
    echo "$sc"
}

# DELETE with cookie; discards response body
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
# Uses python3 (already a prerequisite).
html_id() {
    local html="$1" prefix="$2"
    echo "$html" | python3 -c "
import sys, re
m = re.search(r'id=\"${prefix}-([0-9a-f-]{36})\"', sys.stdin.read())
print(m.group(1) if m else '')
" 2>/dev/null
}

# ── Prerequisites ─────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}TinyDM — Phase 6 Admin Web UI Integration Tests${NC}"
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

printf 'Phase 6 test file — alpha\n' > "$WORK/alpha.txt"
printf 'Phase 6 test file — beta\n'  > "$WORK/beta.txt"

RUN_ID=$(date +%s)

# API token — obtained once at startup; used for generating audit events and cleanup.
echo ""
echo -e "  Obtaining API token for setup / cleanup…"
_tok_body=$(curl -sf -X POST \
    -H "Content-Type: application/json" \
    -d "{\"tenant_id\":\"$TENANT_ID\",\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" \
    "$BASE_URL/api/v1/auth/login" 2>/dev/null || true)
API_TOKEN=$(echo "$_tok_body" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])" 2>/dev/null || true)

if [[ -z "$API_TOKEN" ]]; then
    echo -e "  ${RED}Could not obtain API token.${NC}"
    echo -e "  ${DIM}Set TINYDM_ADMIN_USER / TINYDM_ADMIN_PASS or restart with bootstrap env vars.${NC}"
    exit 1
fi
echo -e "  ${GREEN}API token issued.${NC}"

# IDs accumulated during the test — used in cleanup
TEST_TENANT_ID=""
TEST_PROJECT_ID=""
TEST_DOC_ID=""

# ─────────────────────────────────────────────────────────────────────────────
section "6.2 — Embedded static assets are served"

echo ""
sc=$(_web GET "/admin/static/style.css")
assert_eq "$sc" "200" "GET /admin/static/style.css → 200"
assert_contains "$(body)" ".sidebar" "style.css contains sidebar rules"
assert_contains "$(body)" ".btn"     "style.css contains button rules"
assert_contains "$(body)" ".card"    "style.css contains card rules"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "6.1 — Unauthenticated requests redirect to /admin/login"

echo ""
for path in "/admin/" "/admin/tenants" "/admin/users" "/admin/apikeys" "/admin/audit"; do
    sc=$(_noauth "$path")
    assert_eq "$sc" "302" "GET $path without session → 302"
done

# ─────────────────────────────────────────────────────────────────────────────
section "6.3 — Login page"

echo ""
sc=$(_web GET "/admin/login")
assert_eq "$sc" "200"             "GET /admin/login → 200"
assert_contains "$(body)" "Sign in"    "Login page has 'Sign in' heading"
assert_contains "$(body)" "tenant_id"  "Login page has tenant_id field"
assert_contains "$(body)" "TinyDM"     "Login page shows branding"
rm -f "$CURL_TMP"

echo ""
echo -e "  Testing empty-field validation…"
sc=$(_web POST "/admin/login" -d "tenant_id=&username=&password=")
assert_eq "$sc" "200" "POST with empty fields → 200 (stays on login page)"
assert_contains "$(body)" "required" "Validation message shown for empty fields"
rm -f "$CURL_TMP"

echo ""
echo -e "  Testing wrong password…"
sc=$(_web POST "/admin/login" \
    -d "tenant_id=$TENANT_ID&username=$ADMIN_USER&password=definitely-wrong")
assert_eq "$sc" "200" "POST with wrong password → 200 (stays on login page)"
assert_contains "$(body)" "Invalid credentials" "Error message shown for bad password"
rm -f "$CURL_TMP"

echo ""
echo -e "  Testing unknown tenant…"
sc=$(_web POST "/admin/login" \
    -d "tenant_id=no-such-tenant&username=$ADMIN_USER&password=$ADMIN_PASS")
assert_eq "$sc" "200" "POST with unknown tenant → 200 (stays on login page)"
assert_contains "$(body)" "Invalid credentials" "Error message shown for unknown tenant"
rm -f "$CURL_TMP"

echo ""
echo -e "  Logging in with correct credentials…"
sc=$(_web POST "/admin/login" \
    -d "tenant_id=$TENANT_ID&username=$ADMIN_USER&password=$ADMIN_PASS")
assert_eq "$sc" "302" "POST with correct credentials → 302 redirect"
rm -f "$CURL_TMP"

if grep -q "tdm_session" "$COOKIE_JAR" 2>/dev/null; then
    pass "Session cookie (tdm_session) saved to cookie jar"
else
    fail "Session cookie (tdm_session) missing from cookie jar after login"
fi

echo ""
echo -e "  Testing that a tampered session is rejected…"
BAD_JAR="$WORK/bad_cookies.txt"
printf 'localhost\tFALSE\t/\tFALSE\t0\ttdm_session\tnot.a.valid.jwt\n' > "$BAD_JAR"
bad_sc=$(curl -s -o /dev/null -w "%{http_code}" \
    -b "$BAD_JAR" "$BASE_URL/admin/")
_show_req "GET(bad)" "/admin/" "$bad_sc"
assert_eq "$bad_sc" "302" "Request with tampered JWT → 302 to login"

# ─────────────────────────────────────────────────────────────────────────────
section "6.4 — Dashboard"

echo ""
sc=$(_web GET "/admin/")
assert_eq "$sc" "200"                "GET /admin/ with valid session → 200"
assert_contains "$(body)" "Dashboard"       "Dashboard heading present"
assert_contains "$(body)" "TinyDM"          "'TinyDM' branding in sidebar"
assert_contains "$(body)" "$ADMIN_USER"     "Logged-in username shown in sidebar"
assert_contains "$(body)" "Tenants"         "Tenants stat card present"
assert_contains "$(body)" "Users"           "Users stat card present"
assert_contains "$(body)" "Projects"        "Projects stat card present"
assert_contains "$(body)" "Documents"       "Documents stat card present"
assert_contains "$(body)" "Recent Activity" "Recent activity section present"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "6.5 — Tenant browser"

echo ""
TENANT_NAME="webtest-$RUN_ID"

echo -e "  Creating a test tenant via HTMX form POST…"
sc=$(_web POST "/admin/tenants" \
    -d "name=$TENANT_NAME&description=Phase+6+test+tenant")
assert_eq "$sc" "200" "POST /admin/tenants → 200 (HTMX row partial)"
TENANT_ROW=$(body)
assert_contains "$TENANT_ROW" "$TENANT_NAME"  "Response contains new tenant name"
assert_contains "$TENANT_ROW" 'id="tenant-'   "Response contains tenant <tr> with id"
TEST_TENANT_ID=$(html_id "$TENANT_ROW" "tenant")
info "Created tenant ID: $TEST_TENANT_ID"
rm -f "$CURL_TMP"

echo ""
echo -e "  Verifying tenant appears on the tenants list page…"
sc=$(_web GET "/admin/tenants")
assert_eq "$sc" "200"                   "GET /admin/tenants → 200"
assert_contains "$(body)" "$TENANT_NAME"     "New tenant name visible in list"
assert_contains "$(body)" "$TEST_TENANT_ID"  "New tenant ID visible in list"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "6.5 — Project browser"

echo ""
PROJECT_NAME="proj-$RUN_ID"

echo -e "  Creating a test project under the default tenant…"
sc=$(_web POST "/admin/tenants/$TENANT_ID/projects" \
    -d "name=$PROJECT_NAME&description=Phase+6+test+project")
assert_eq "$sc" "200" "POST /admin/tenants/{id}/projects → 200 (HTMX row partial)"
PROJECT_ROW=$(body)
assert_contains "$PROJECT_ROW" "$PROJECT_NAME"  "Response contains new project name"
assert_contains "$PROJECT_ROW" 'id="project-'   "Response contains project <tr> with id"
TEST_PROJECT_ID=$(html_id "$PROJECT_ROW" "project")
info "Created project ID: $TEST_PROJECT_ID"
rm -f "$CURL_TMP"

echo ""
echo -e "  Verifying project appears on the projects page…"
sc=$(_web GET "/admin/tenants/$TENANT_ID/projects")
assert_eq "$sc" "200"                    "GET /admin/tenants/{id}/projects → 200"
assert_contains "$(body)" "$PROJECT_NAME"     "New project name visible in list"
assert_contains "$(body)" "$TENANT_ID"        "Tenant name in page header"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "6.5 — Bucket browser"

echo ""
BUCKET_NAME="bucket-$RUN_ID"

echo -e "  Creating a test bucket under the test project…"
sc=$(_web POST "/admin/tenants/$TENANT_ID/projects/$TEST_PROJECT_ID/buckets" \
    -d "name=$BUCKET_NAME&description=Phase+6+test+bucket")
assert_eq "$sc" "200" "POST …/projects/{id}/buckets → 200 (HTMX row partial)"
BUCKET_ROW=$(body)
assert_contains "$BUCKET_ROW" "$BUCKET_NAME"  "Response contains new bucket name"
assert_contains "$BUCKET_ROW" 'id="bucket-'   "Response contains bucket <tr> with id"
TEST_BUCKET_ID=$(html_id "$BUCKET_ROW" "bucket")
info "Created bucket ID: $TEST_BUCKET_ID"
rm -f "$CURL_TMP"

echo ""
echo -e "  Verifying bucket appears on the buckets page…"
sc=$(_web GET "/admin/tenants/$TENANT_ID/projects/$TEST_PROJECT_ID/buckets")
assert_eq "$sc" "200"                    "GET …/projects/{id}/buckets → 200"
assert_contains "$(body)" "$BUCKET_NAME"     "New bucket name visible in list"
assert_contains "$(body)" "$PROJECT_NAME"    "Project name in breadcrumb"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "6.6 — Document management"

echo ""
DOCS_PATH="/admin/tenants/$TENANT_ID/projects/$TEST_PROJECT_ID/buckets/$TEST_BUCKET_ID/documents"

echo -e "  Checking documents page loads with breadcrumb…"
sc=$(_web GET "$DOCS_PATH")
assert_eq "$sc" "200"                    "GET documents page → 200"
assert_contains "$(body)" "Documents"        "Documents heading visible"
assert_contains "$(body)" "$BUCKET_NAME"     "Bucket name in breadcrumb"
assert_contains "$(body)" "$PROJECT_NAME"    "Project name in breadcrumb"
assert_contains "$(body)" "No documents yet" "Empty-state shown for new bucket"
rm -f "$CURL_TMP"

echo ""
echo -e "  Uploading alpha.txt via multipart form POST…"
sc=$(_web POST "$DOCS_PATH" \
    -F "file=@$WORK/alpha.txt" \
    -F "name=alpha-$RUN_ID.txt")
assert_eq "$sc" "200" "POST document upload → 200 (HTMX row partial)"
DOC_ROW=$(body)
assert_contains "$DOC_ROW" "alpha-$RUN_ID.txt"  "Response contains uploaded filename"
assert_contains "$DOC_ROW" 'id="doc-'            "Response contains doc <tr> with id"
assert_contains "$DOC_ROW" "text/plain"           "Content-type shown in row"
TEST_DOC_ID=$(html_id "$DOC_ROW" "doc")
info "Uploaded document ID: $TEST_DOC_ID"
rm -f "$CURL_TMP"

echo ""
echo -e "  Uploading beta.txt (filename used as name when override is blank)…"
sc=$(_web POST "$DOCS_PATH" -F "file=@$WORK/beta.txt")
assert_eq "$sc" "200" "Second upload (no name override) → 200"
BETA_ROW=$(body)
assert_contains "$BETA_ROW" "beta.txt" "Filename used as document name"
BETA_DOC_ID=$(html_id "$BETA_ROW" "doc")
info "Beta document ID: $BETA_DOC_ID"
rm -f "$CURL_TMP"

echo ""
echo -e "  Downloading the uploaded alpha file…"
sc=$(_web GET "/admin/documents/$TEST_DOC_ID/download")
assert_eq "$sc" "200" "GET /admin/documents/{id}/download → 200"
assert_contains "$(body)" "Phase 6 test file" "Downloaded file content matches uploaded content"
rm -f "$CURL_TMP"

echo ""
echo -e "  Deleting the beta document…"
sc=$(_web_del "/admin/documents/$BETA_DOC_ID")
assert_eq "$sc" "200" "DELETE /admin/documents/{id} → 200 (HTMX removes row)"

# ─────────────────────────────────────────────────────────────────────────────
section "6.7 — User management"

echo ""
TEST_USER="webtest-$RUN_ID"

echo -e "  Checking users page loads…"
sc=$(_web GET "/admin/users")
assert_eq "$sc" "200"                 "GET /admin/users → 200"
assert_contains "$(body)" "Users"         "Users heading visible"
assert_contains "$(body)" "$ADMIN_USER"   "Bootstrap admin shown in user list"
rm -f "$CURL_TMP"

echo ""
echo -e "  Creating a new user via form POST…"
sc=$(_web POST "/admin/users" \
    -d "username=$TEST_USER&email=${TEST_USER}@example.com&password=testpass123&role=user")
assert_eq "$sc" "200" "POST /admin/users → 200 (HTMX row partial)"
USER_ROW=$(body)
assert_contains "$USER_ROW" "$TEST_USER"  "Response contains new username"
assert_contains "$USER_ROW" 'id="user-'   "Response contains user <tr> with id"
assert_contains "$USER_ROW" "Active"      "New user is active by default"
assert_contains "$USER_ROW" "User"        "New user has 'User' role badge"
TEST_USER_ID=$(html_id "$USER_ROW" "user")
info "Created user ID: $TEST_USER_ID"
rm -f "$CURL_TMP"

echo ""
echo -e "  Deactivating the test user…"
sc=$(_web POST "/admin/users/$TEST_USER_ID/deactivate")
assert_eq "$sc" "200" "POST …/deactivate → 200 (HTMX row partial)"
assert_contains "$(body)" "Inactive"  "Deactivated user shows 'Inactive' badge"
assert_not_contains "$(body)" "Deactivate" "Deactivate button gone when inactive"
rm -f "$CURL_TMP"

echo ""
echo -e "  Re-activating the test user…"
sc=$(_web POST "/admin/users/$TEST_USER_ID/activate")
assert_eq "$sc" "200" "POST …/activate → 200 (HTMX row partial)"
assert_contains "$(body)" "Active"    "Reactivated user shows 'Active' badge"
assert_contains "$(body)" "Deactivate" "Deactivate button visible again"
rm -f "$CURL_TMP"

echo ""
echo -e "  Deleting the test user…"
sc=$(_web_del "/admin/users/$TEST_USER_ID")
assert_eq "$sc" "200" "DELETE /admin/users/{id} → 200"

# ─────────────────────────────────────────────────────────────────────────────
section "6.8 — API key management"

echo ""
KEY_NAME="webtest-key-$RUN_ID"

echo -e "  Checking API keys page loads…"
sc=$(_web GET "/admin/apikeys")
assert_eq "$sc" "200"           "GET /admin/apikeys → 200"
assert_contains "$(body)" "API Keys" "API Keys heading visible"
rm -f "$CURL_TMP"

echo ""
echo -e "  Generating a new API key…"
sc=$(_web POST "/admin/apikeys" -d "name=$KEY_NAME")
assert_eq "$sc" "200" "POST /admin/apikeys → 200 (full page with plaintext key)"
KEY_PAGE=$(body)
assert_contains "$KEY_PAGE" "tdm_"         "Plaintext API key shown (tdm_ prefix)"
assert_contains "$KEY_PAGE" "copy it now"  "One-time-display warning present"
assert_contains "$KEY_PAGE" "$KEY_NAME"    "New key name visible in key list"
assert_contains "$KEY_PAGE" "Active"       "New key shows 'Active' badge"
TEST_KEY_ID=$(html_id "$KEY_PAGE" "key")
info "Created key ID: $TEST_KEY_ID"
rm -f "$CURL_TMP"

echo ""
echo -e "  Revisiting the API keys page — plaintext should no longer be shown…"
sc=$(_web GET "/admin/apikeys")
assert_eq "$sc" "200" "GET /admin/apikeys (revisit) → 200"
assert_not_contains "$(body)" "copy it now" "One-time key warning absent on page revisit"
assert_contains "$(body)" "$KEY_NAME"       "Key name still shown in list"
rm -f "$CURL_TMP"

echo ""
echo -e "  Revoking the API key…"
sc=$(_web POST "/admin/apikeys/$TEST_KEY_ID/revoke")
assert_eq "$sc" "200" "POST …/revoke → 200 (HTMX row partial)"
assert_contains "$(body)" "Revoked" "Revoked key shows 'Revoked' badge"
assert_not_contains "$(body)" "hx-confirm" "Revoke button gone after revocation"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "6.9 — Audit log viewer"

echo ""
echo -e "  Seeding at least one guaranteed audit event via the REST API…"
_seed_proj=$(curl -sf \
    -X POST \
    -H "Authorization: Bearer $API_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"audit-seed-$RUN_ID\",\"description\":\"audit log viewer test seed\"}" \
    "$BASE_URL/api/v1/tenants/$TENANT_ID/projects" 2>/dev/null || true)
SEED_PROJECT_ID=$(echo "$_seed_proj" | python3 -c \
    "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || true)
info "Seeded project ID: $SEED_PROJECT_ID"
sleep 0.3   # let the async audit write flush

echo ""
echo -e "  Checking audit log page loads with filter bar…"
sc=$(_web GET "/admin/audit")
assert_eq "$sc" "200"                   "GET /admin/audit → 200"
assert_contains "$(body)" "Audit Log"       "Audit Log heading visible"
assert_contains "$(body)" "filter-bar"      "Filter bar element present"
assert_contains "$(body)" "filter-action"   "Action filter input present"
assert_contains "$(body)" "filter-principal" "Principal filter input present"
assert_contains "$(body)" "filter-from"     "From date filter input present"
assert_contains "$(body)" "filter-limit"    "Limit dropdown present"
rm -f "$CURL_TMP"

echo ""
echo -e "  Fetching audit events partial with no filters (HTMX endpoint)…"
sc=$(_web GET "/admin/audit/events")
assert_eq "$sc" "200" "GET /admin/audit/events → 200"
assert_contains "$(body)" "<tr" "Audit events partial contains table rows"
rm -f "$CURL_TMP"

echo ""
echo -e "  Filtering by action 'project.*' (matches seeded event)…"
sc=$(_web GET "/admin/audit/events?action=project.*&limit=50")
assert_eq "$sc" "200" "GET /admin/audit/events?action=project.* → 200"
assert_contains "$(body)" "project" "Rows contain project-scoped actions"
rm -f "$CURL_TMP"

echo ""
echo -e "  Filtering by principal '$ADMIN_USER'…"
sc=$(_web GET "/admin/audit/events?principal=$ADMIN_USER&limit=50")
assert_eq "$sc" "200" "GET /admin/audit/events?principal=$ADMIN_USER → 200"
assert_contains "$(body)" "<tr" "Events returned for known principal"
rm -f "$CURL_TMP"

echo ""
echo -e "  Checking limit=1 returns exactly one row…"
sc=$(_web GET "/admin/audit/events?limit=1")
assert_eq "$sc" "200" "GET /admin/audit/events?limit=1 → 200"
ROW_COUNT=$(body | grep -c "<tr" || true)
assert_eq "$ROW_COUNT" "1" "limit=1 returns exactly 1 <tr>"
rm -f "$CURL_TMP"

echo ""
echo -e "  Unknown principal should return empty state…"
sc=$(_web GET "/admin/audit/events?principal=no-such-user-xyz")
assert_eq "$sc" "200" "GET /admin/audit/events with unknown principal → 200"
assert_contains "$(body)" "No events found" "Empty state shown when no events match"
rm -f "$CURL_TMP"

# ─────────────────────────────────────────────────────────────────────────────
section "6.1 — Logout clears the session"

echo ""
echo -e "  Logging out…"
sc=$(_web GET "/admin/logout")
assert_eq "$sc" "302" "GET /admin/logout → 302 redirect to login"
rm -f "$CURL_TMP"

echo ""
echo -e "  Verifying the dashboard is no longer accessible…"
sc=$(_web GET "/admin/")
assert_eq "$sc" "302" "GET /admin/ after logout → 302 (session cleared)"
rm -f "$CURL_TMP"

echo ""
echo -e "  Verifying the login page is still accessible after logout…"
sc=$(_web GET "/admin/login")
assert_eq "$sc" "200" "GET /admin/login after logout → 200"
rm -f "$CURL_TMP"

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

# Seed project created for audit event generation
[[ -n "$SEED_PROJECT_ID" ]] && \
    _api_del "/api/v1/tenants/$TENANT_ID/projects/$SEED_PROJECT_ID"

# Test project (and its bucket / documents)
[[ -n "$TEST_PROJECT_ID" ]] && \
    _api_del "/api/v1/tenants/$TENANT_ID/projects/$TEST_PROJECT_ID"

# Test tenant (created in the browser; use API delete)
[[ -n "$TEST_TENANT_ID" ]] && \
    _api_del "/api/v1/tenants/$TEST_TENANT_ID"

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
