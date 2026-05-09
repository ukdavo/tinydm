#!/usr/bin/env bash
# One-shot script to commit and push Phase 7 work.
# Run from within the tinydm repo directory.
set -e
cd "$(dirname "$0")"

# Clean up any stale git worktree lock from a previous session
rm -f .git/index.lock

git add -A
git commit -m "Phase 7: Document & bucket management UI

- Bucket inline edit: GET /edit partial, GET /row partial, PUT rename+desc
- Document inline edit: rename-only (no snapshot) and content replace (with snapshot)
- Document search: HTMX rows partial with ?q= name filter, debounced 300ms
- Tag filter: HTMX rows partial with ?tag= filter, combinable with ?q=
- Document detail page: metadata grid, tags, properties, version history
- Tag management: add/remove chips via HTMX partials
- Custom properties: set/delete with HTMX partial updates
- System metadata: read-only sys.* panel on detail page
- Version history & restore with hx-confirm dialog
- test_phase7.sh: 50+ assertions covering all nine UI sections
- PLAN.md: Phase 7 marked complete"

git push
echo "Done."
