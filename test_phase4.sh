#!/usr/bin/env bash
# test_phase4.sh — Integration tests for TinyDM Phase 4 features.
#
# Covers: version restore, tags, custom properties, and auto-metadata extraction.
# Prerequisites: curl, python3
# The server must already be running before executing this script.
#
# Usage:
#   ./test_phase4.sh [BASE_URL]
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

assert_contains() {
    local body="$1" needle="$2" msg="$3"
    if echo "$body" | grep -qF "$needle"; then
        pass "$msg"
    else
        fail "$msg"
        echo -e "    ${DIM}expected to find: $needle${NC}"
        echo -e "    ${DIM}in: ${body:0:300}${NC}"
    fi
}

assert_not_contains() {
    local body="$1" needle="$2" msg="$3"
    if ! echo "$body" | grep -qF "$needle"; then
        pass "$msg"
    else
        fail "$msg"
        echo -e "    ${DIM}did not expect: $needle${NC}"
    fi
}

# ── HTTP helpers ──────────────────────────────────────────────────────────────
# All helpers read AUTH_ARGS (populated after login).
AUTH_ARGS=()

# Standard JSON request/response.
_get()     { curl -sf ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$BASE_URL$1"; }
_post()    { curl -sf -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} -H "Content-Type: application/json" -d "$2" "$BASE_URL$1"; }
_put()     { curl -sf -X PUT  ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} -H "Content-Type: application/json" -d "$2" "$BASE_URL$1"; }

# Multipart form-data uploads: path first, then -F flags.
_post_mp() { local path=$1; shift; curl -sf -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$@" "$BASE_URL$path"; }
_put_mp()  { local path=$1; shift; curl -sf -X PUT  ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$@" "$BASE_URL$path"; }

# No-body POST (e.g. restore action).
_post_e()  { curl -sf -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$BASE_URL$1"; }

# Status-code-only variants (do not fail on 4xx/5xx).
_sc_get()  { curl -s -o /dev/null -w "%{http_code}" ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$BASE_URL$1"; }
_sc_del()  { curl -s -o /dev/null -w "%{http_code}" -X DELETE ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$BASE_URL$1"; }
_sc_put()  { curl -s -o /dev/null -w "%{http_code}" -X PUT ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} \
                  -H "Content-Type: application/json" -d "$2" "$BASE_URL$1"; }

# Extract a field from a JSON string piped to stdin.
jfield() { python3 -c "import sys,json; print(json.load(sys.stdin)$1)" 2>/dev/null; }
jlen()   { python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null; }

# ── Prerequisites ─────────────────────────────────────────────────────────────
echo -e "\n${BOLD}TinyDM — Phase 4 Integration Tests${NC}"
echo    "Server : $BASE_URL"

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

info "Generating test fixtures…"

# 1×1 red RGB PNG (pure Python3, no external tools)
python3 - << 'PY' > "$WORK/red1x1.png"
import struct, zlib, sys
def chunk(tag, data):
    p = tag + data
    return struct.pack('>I', len(data)) + p + struct.pack('>I', zlib.crc32(p) & 0xffffffff)
png  = b'\x89PNG\r\n\x1a\n'
png += chunk(b'IHDR', struct.pack('>IIBBBBB', 1, 1, 8, 2, 0, 0, 0))
# Filter byte (None=0) + R=255 G=0 B=0
png += chunk(b'IDAT', zlib.compress(b'\x00\xff\x00\x00'))
png += chunk(b'IEND', b'')
sys.stdout.buffer.write(png)
PY

# 2×2 blue RGB PNG (used to verify metadata updates on re-upload)
python3 - << 'PY' > "$WORK/blue2x2.png"
import struct, zlib, sys
def chunk(tag, data):
    p = tag + data
    return struct.pack('>I', len(data)) + p + struct.pack('>I', zlib.crc32(p) & 0xffffffff)
png  = b'\x89PNG\r\n\x1a\n'
png += chunk(b'IHDR', struct.pack('>IIBBBBB', 2, 2, 8, 2, 0, 0, 0))
# Each row: filter byte (0) + 2 × (R=0 G=0 B=255)
row  = b'\x00\x00\x00\xff\x00\x00\xff'
png += chunk(b'IDAT', zlib.compress(row + row))
png += chunk(b'IEND', b'')
sys.stdout.buffer.write(png)
PY

# Minimal valid PDF (just enough for header/version extraction)
printf '%%PDF-1.7\n1 0 obj<</Type/Catalog>>endobj\nxref\n0 2\n0000000000 65535 f\r\n0000000009 00000 n\r\ntrailer<</Size 2/Root 1 0 R>>\nstartxref\n9\n%%%%EOF\n' \
    > "$WORK/sample.pdf"

printf 'Version 1 — original content\n' > "$WORK/v1.txt"
printf 'Version 2 — updated content\n'  > "$WORK/v2.txt"

# ─────────────────────────────────────────────────────────────────────────────
section "Authentication"

login_resp=$(_post "/api/v1/auth/login" \
    "{\"tenant_id\":\"$TENANT_ID\",\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}")
TOKEN=$(echo "$login_resp" | jfield "['token']" || true)
if [[ -z "$TOKEN" ]]; then
    fail "Login failed — aborting"
    exit 1
fi
AUTH_ARGS=(-H "Authorization: Bearer $TOKEN")
pass "Login succeeded, JWT issued"

# ─────────────────────────────────────────────────────────────────────────────
section "Test Setup"

RUN_ID=$(date +%s)

proj=$(_post "/api/v1/tenants/$TENANT_ID/projects" \
    "{\"name\":\"phase4-$RUN_ID\",\"description\":\"Phase 4 integration tests\"}")
PROJECT_ID=$(echo "$proj" | jfield "['id']")
pass "Created project  $PROJECT_ID"

bkt=$(_post "/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID/buckets" \
    '{"name":"tests","description":""}')
BUCKET_ID=$(echo "$bkt" | jfield "['id']")
pass "Created bucket   $BUCKET_ID"

DOCS="/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID/buckets/$BUCKET_ID/documents"

# ─────────────────────────────────────────────────────────────────────────────
section "4.3 — Version Restore"

# Upload v1
doc=$(_post_mp "$DOCS" -F "file=@$WORK/v1.txt" -F "name=restore-test.txt")
DOC_ID=$(echo "$doc" | jfield "['id']")
assert_eq "$(echo "$doc" | jfield "['version']")" "1" \
    "Upload creates document at version 1"

# Update to v2 — snapshots current state (v1) and advances version counter
doc=$(_put_mp "$DOCS/$DOC_ID" -F "file=@$WORK/v2.txt")
assert_eq "$(echo "$doc" | jfield "['version']")" "2" \
    "First update advances document to version 2"

# Version history should contain one snapshot
versions=$(_get "$DOCS/$DOC_ID/versions")
assert_eq "$(echo "$versions" | jlen)" "1" \
    "One version snapshot exists after update"

V1_SNAP_ID=$(echo "$versions" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])")
assert_eq "$(echo "$versions" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['version'])")" "1" \
    "Snapshot records version number 1"

# Verify current content is v2 before restoring
content=$(_get "$DOCS/$DOC_ID/content")
assert_contains "$content" "Version 2" \
    "Current content is v2 before restore"

# Restore to v1
restored=$(_post_e "$DOCS/$DOC_ID/versions/$V1_SNAP_ID/restore")
assert_eq "$(echo "$restored" | jfield "['version']")" "3" \
    "Restore creates version 3 (snapshots v2 first, then applies v1)"

# Content should now be v1 again
content=$(_get "$DOCS/$DOC_ID/content")
assert_contains "$content" "Version 1" \
    "Content after restore matches original v1"

# v2 snapshot should now also exist
versions=$(_get "$DOCS/$DOC_ID/versions")
assert_eq "$(echo "$versions" | jlen)" "2" \
    "Two snapshots exist after restore (v1 + v2)"

# ─────────────────────────────────────────────────────────────────────────────
section "4.5 — Tag Support"

tag_doc=$(_post_mp "$DOCS" -F "file=@$WORK/v1.txt" -F "name=tag-test.txt")
TDOC=$(echo "$tag_doc" | jfield "['id']")

# New document has no tags
assert_eq "$(_get "$DOCS/$TDOC/tags")" "[]" \
    "Tags list is empty on new document"

# Add two tags
resp=$(curl -sf -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$BASE_URL$DOCS/$TDOC/tags/draft")
assert_contains "$resp" '"draft"' \
    "POST /tags/draft — tag added"

resp=$(curl -sf -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$BASE_URL$DOCS/$TDOC/tags/urgent")
assert_contains "$resp" '"draft"'  "POST /tags/urgent — first tag still present"
assert_contains "$resp" '"urgent"' "POST /tags/urgent — second tag present"

assert_eq "$(echo "$resp" | jlen)" "2" \
    "GET /tags returns 2 tags"

# Filter documents by tag
filtered=$(_get "$DOCS?tag=draft")
assert_contains "$filtered" "$TDOC" \
    "?tag=draft filter returns the tagged document"

filtered=$(_get "$DOCS?tag=no-such-tag-xyz")
assert_eq "$filtered" "[]" \
    "?tag= with no matches returns empty array"

# Remove one tag
assert_eq "$(_sc_del "$DOCS/$TDOC/tags/urgent")" "204" \
    "DELETE /tags/urgent — 204 No Content"

tags=$(_get "$DOCS/$TDOC/tags")
assert_not_contains "$tags" '"urgent"' "Tag 'urgent' removed"
assert_contains     "$tags" '"draft"'  "Tag 'draft' still present"

# Bulk replace
tags=$(_put "$DOCS/$TDOC/tags" '{"tags":["approved","final","archived"]}')
assert_eq "$(echo "$tags" | jlen)" "3" \
    "PUT /tags replaces all tags — 3 new tags"
assert_not_contains "$tags" '"draft"'    "Old tag 'draft' gone after replace"
assert_contains     "$tags" '"approved"' "New tag 'approved' present"

# Clear all tags
assert_eq "$(_put "$DOCS/$TDOC/tags" '{"tags":[]}')" "[]" \
    "PUT /tags [] clears all tags"

# ─────────────────────────────────────────────────────────────────────────────
section "4.6 — Custom Properties"

pdoc=$(_post_mp "$DOCS" -F "file=@$WORK/v1.txt" -F "name=prop-test.txt")
PDOC=$(echo "$pdoc" | jfield "['id']")

# Set a property
assert_eq "$(_sc_put "$DOCS/$PDOC/properties/author" '{"value":"Alice"}')" "200" \
    "PUT /properties/author — 200 OK"

props=$(_get "$DOCS/$PDOC/properties")
assert_contains "$props" '"author"' "GET /properties contains key 'author'"
assert_contains "$props" '"Alice"'  "GET /properties contains value 'Alice'"

# Add a second property
_put "$DOCS/$PDOC/properties/department" '{"value":"Engineering"}' > /dev/null
props=$(_get "$DOCS/$PDOC/properties")
assert_contains "$props" '"Engineering"' "Second property 'department' stored"

# Bulk replace — removes 'department', sets author=Bob, adds project=alpha
props=$(_put "$DOCS/$PDOC/properties" '{"author":"Bob","project":"alpha"}')
assert_contains     "$props" '"Bob"'        "Bulk PUT updates 'author' to Bob"
assert_contains     "$props" '"alpha"'      "Bulk PUT adds key 'project'"
assert_not_contains "$props" '"department"' "Bulk PUT removes key absent from new set"

# Delete a single property
assert_eq "$(_sc_del "$DOCS/$PDOC/properties/project")" "204" \
    "DELETE /properties/project — 204 No Content"
props=$(_get "$DOCS/$PDOC/properties")
assert_not_contains "$props" '"project"' "Deleted key 'project' no longer present"
assert_contains     "$props" '"author"'  "Key 'author' unaffected by delete"

# Reserved sys.* namespace — writes must be blocked
assert_eq "$(_sc_put "$DOCS/$PDOC/properties/sys.checksum" '{"value":"hack"}')" "403" \
    "PUT /properties/sys.checksum returns 403 (reserved namespace)"
assert_eq "$(_sc_del "$DOCS/$PDOC/properties/sys.size")" "403" \
    "DELETE /properties/sys.size returns 403 (reserved namespace)"

# ─────────────────────────────────────────────────────────────────────────────
section "4.7 — Image Metadata Extraction"

img_doc=$(_post_mp "$DOCS" -F "file=@$WORK/red1x1.png" -F "name=red1x1.png")
IDOC=$(echo "$img_doc" | jfield "['id']")
assert_contains "$img_doc" "image/png" \
    "PNG upload — content_type detected as image/png"

props=$(_get "$DOCS/$IDOC/properties")
assert_contains "$props" '"image.width"'  "image.width  auto-extracted on upload"
assert_contains "$props" '"image.height"' "image.height auto-extracted on upload"
assert_contains "$props" '"image.format"' "image.format auto-extracted on upload"
assert_contains "$props" '"image.width":"1"'   "image.width  is 1 (1×1 test image)"
assert_contains "$props" '"image.height":"1"'  "image.height is 1 (1×1 test image)"
assert_contains "$props" '"image.format":"png"' "image.format is png"

# Update with a 2×2 image — metadata properties should be refreshed
_put_mp "$DOCS/$IDOC" -F "file=@$WORK/blue2x2.png" > /dev/null
props=$(_get "$DOCS/$IDOC/properties")
assert_contains "$props" '"image.width":"2"'  "image.width  updated to 2 after re-upload"
assert_contains "$props" '"image.height":"2"' "image.height updated to 2 after re-upload"

# ─────────────────────────────────────────────────────────────────────────────
section "4.8 — PDF Metadata Extraction"

pdf_doc=$(_post_mp "$DOCS" -F "file=@$WORK/sample.pdf" -F "name=sample.pdf")
FDOC=$(echo "$pdf_doc" | jfield "['id']")

props=$(_get "$DOCS/$FDOC/properties")
assert_contains "$props" '"pdf.version"'     "pdf.version property extracted from PDF upload"
assert_contains "$props" '"pdf.version":"1.7"' "pdf.version value is 1.7"

# ─────────────────────────────────────────────────────────────────────────────
section "Cleanup"

assert_eq "$(_sc_del "/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID")" "204" \
    "Deleted test project (cascades to bucket + all documents)"

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
