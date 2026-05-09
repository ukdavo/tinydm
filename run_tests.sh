#!/usr/bin/env bash
# run_tests.sh — Run all TinyDM integration test suites.
#
# Each suite is run in order; failures are recorded but execution continues
# so you get a full picture in one pass.
#
# Usage:
#   ./run_tests.sh [BASE_URL]
#   BASE_URL defaults to http://localhost:8080
#
# Environment variables forwarded to every suite:
#   TINYDM_URL          server base URL (overrides positional arg)
#   TINYDM_ADMIN_USER   admin username   (default: admin)
#   TINYDM_ADMIN_PASS   admin password   (default: changeme)

set -eo pipefail

BASE_URL="${1:-${TINYDM_URL:-http://localhost:8080}}"
export TINYDM_URL="$BASE_URL"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# ── Colours ───────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'; RED='\033[0;31m'; CYAN='\033[0;36m'
BOLD='\033[1m'; DIM='\033[2m'; NC='\033[0m'

# ── Test suites (in run order) ────────────────────────────────────────────────
SUITES=(
    test_phase4.sh
    test_phase5.sh
    test_phase6.sh
    test_phase7.sh
    test_pagination.sh
)

# ── Preflight ─────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}TinyDM — Full Integration Test Run${NC}"
echo    "Server  : $BASE_URL"
echo    "Suites  : ${#SUITES[@]}"
echo ""

health=$(curl -sf "$BASE_URL/health" 2>/dev/null || true)
if [[ "$health" != *'"ok"'* ]]; then
    echo -e "${RED}${BOLD}Server not reachable at $BASE_URL — run ./run.sh first.${NC}"
    exit 1
fi
echo -e "Health  : ${GREEN}ok${NC}"
echo ""

# ── Run each suite ────────────────────────────────────────────────────────────
SUITE_PASS=0
SUITE_FAIL=0
FAILED_SUITES=()

for suite in "${SUITES[@]}"; do
    script="$SCRIPT_DIR/$suite"
    if [[ ! -f "$script" ]]; then
        echo -e "${RED}Missing: $suite — skipping${NC}"
        SUITE_FAIL=$((SUITE_FAIL + 1))
        FAILED_SUITES+=("$suite (missing)")
        continue
    fi

    echo -e "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${CYAN}${BOLD}▶  $suite${NC}"
    echo -e "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    set +e
    bash "$script" "$BASE_URL"
    exit_code=$?
    set -e

    echo ""
    if [[ "$exit_code" -eq 0 ]]; then
        echo -e "  ${GREEN}${BOLD}$suite — PASSED${NC}"
        SUITE_PASS=$((SUITE_PASS + 1))
    else
        echo -e "  ${RED}${BOLD}$suite — FAILED (exit $exit_code)${NC}"
        SUITE_FAIL=$((SUITE_FAIL + 1))
        FAILED_SUITES+=("$suite")
    fi
    echo ""
done

# ── Grand summary ─────────────────────────────────────────────────────────────
echo -e "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo -e "${BOLD}Results${NC}"
echo -e "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo -e "  Suites passed : ${GREEN}${BOLD}$SUITE_PASS${NC}"
echo -e "  Suites failed : ${RED}${BOLD}$SUITE_FAIL${NC}"
echo ""

if [[ "$SUITE_FAIL" -gt 0 ]]; then
    echo -e "  ${RED}Failed suites:${NC}"
    for s in "${FAILED_SUITES[@]}"; do
        echo -e "    ${DIM}• $s${NC}"
    done
    echo ""
    exit 1
else
    echo -e "  ${GREEN}${BOLD}All suites passed.${NC}"
    echo ""
fi
