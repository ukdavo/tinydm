#!/usr/bin/env bash
# Commit the pagination test fixes and new test_pagination.sh.
set -e
cd "$(dirname "$0")"

rm -f .git/index.lock

git add -A
git commit -m "Tests: fix envelope-awareness + add test_pagination.sh

test_phase4.sh / test_phase5.sh — fix breakage introduced by pagination:
- jlen() is now envelope-aware: unwraps {\"data\":[...]} before counting
- jdata() helper added: extracts .data[] from paginated responses as a
  JSON array for piping into subsequent python expressions
- jfield() unchanged (operates on single-object responses)
- Inline python blocks that iterate audit events now unwrap .data first:
    d = json.load(sys.stdin)
    events = d['data'] if isinstance(d, dict) and 'data' in d else d
- assert_eq \"\$var\" \"[]\" comparisons replaced with jlen == 0 pattern
  (bare \"[]\" never matches a paginated envelope)
- Version-history indexing ([0]['id'], [0]['version']) piped through
  jdata before indexing into the array
- Tags endpoints remain bare-array (not paginated) — those assertions
  left unchanged

test_pagination.sh — new dedicated pagination test suite:
- P.1  Envelope shape on all list endpoints (data[], pagination{})
- P.2  limit= controls page size; pagination.limit echoes back
- P.3  offset= skips items; pages don't overlap
- P.4  pagination.total reflects the full count regardless of limit
- P.5  offset beyond total returns empty data[], total still correct
- P.6  has_more semantics (true mid-set, false on last page)
- P.7  /versions endpoint is paginated
- P.8  /audit endpoint pagination (limit, offset, out-of-bounds)
- P.9  /users and /apikeys pagination
- P.10 Default limit is 50 for documents and audit
- P.11 ?q= search and ?tag= filter also return paginated envelopes"

git push
echo "Done."
