#!/usr/bin/env bash
# One-shot script to commit and push pagination work.
# Run from within the tinydm repo directory.
set -e
cd "$(dirname "$0")"

rm -f .git/index.lock

git add -A
git commit -m "Pagination: REST API envelope + web UI pager bars

REST API changes:
- repo/store.go: ListTenants, ListProjects, ListBuckets, ListDocuments,
  SearchDocuments, ListDocumentsByTag, ListDocumentVersions now accept
  PageOpts{Limit, Offset} and return (items, total, error)
- auth/store.go: ListUsers and ListAPIKeys accept limit/offset int params
  and return (items, total, error)
- audit/store.go: List returns ([]*Event, int, error) with COUNT(*) total
- api/handler.go: pageParams(), PagedMeta, pagedResponse, writePaged()
- All REST List endpoints return {data:[...], pagination:{total,limit,offset,has_more}}
  with ?limit= and ?offset= query param support
- audit API endpoint normalises limit/offset before passing to store

Web UI changes:
- web/handlers.go: WebPagination struct, parsePage(), newWebPagination(),
  pageOffset() helpers
- All list page handlers (tenants, projects, buckets, documents, users,
  apikeys, audit) use ?page=N pagination with WebPagination in template data
- documentRows HTMX partial: paginated + emits docs-pager-oob OOB swap
- auditEvents HTMX partial: paginated + emits audit-pager-oob OOB swap,
  preserves active filter params in pagination links
- buildDocDetailData: ListDocumentVersions with limit=500

Template changes:
- base.html: {{define \"pagination\"}} shared bar (prev/next + total info)
- tenants, projects, buckets, users, apikeys: {{template \"pagination\" .Pager}}
- documents.html: #docs-pagination div + docs-pager-oob OOB partial
- audit.html: #audit-pagination div + audit-pager-oob OOB partial
- style.css: .pagination, .pagination-info, .pagination-links, .disabled styles"

git push
echo "Done."
