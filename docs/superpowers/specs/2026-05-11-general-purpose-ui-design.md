# General-Purpose UI Design Spec

**Date:** 2026-05-11
**Status:** Approved
**Scope:** Rename the admin UI context from `/admin` to `/app`, allow non-admin users to log in, and hide admin-only functionality based on role.

---

## Background

The TinyDM web UI lives under `/admin/*` and is labelled "TinyDM Admin". In practice, regular `user`-role accounts also need access to the UI — to browse projects, upload and manage documents. Restricting the URL prefix and session middleware to admins only was never done, but the UI never hid admin-only controls from non-admins either. This spec formalises the split.

---

## Goals

- Rename all `/admin/*` routes and hrefs to `/app/*`.
- Allow both `admin` and `user` roles to log in via the web UI.
- Protect admin-only routes server-side with a `requireAdmin` middleware.
- Hide admin-only nav items and action buttons from non-admin users via template conditionals.

## Non-Goals

- No RBAC-based data filtering in the web UI (the existing `TINYDM_PERM_MODE` env var controls API-layer access; the web UI shows all projects/buckets/documents regardless of RBAC rights).
- No change to the API (`/api/v1/*`) routes.
- No new pages or features beyond visibility changes.

---

## URL Rename

Every `/admin` prefix in the web package becomes `/app`. Affected locations:

| Location | Change |
|----------|--------|
| `internal/web/web.go` — `RegisterRoutes` | All route paths |
| `internal/web/web.go` — `requireSession` redirect | `/admin/login` → `/app/login` |
| `internal/web/web.go` — static assets | `/admin/static/*` → `/app/static/*` |
| `internal/web/handlers.go` — `loginSubmit` redirect | `/admin/` → `/app/` |
| `internal/web/handlers.go` — `logout` redirect | `/admin/login` → `/app/login` |
| `internal/web/templates/base.html` | CSS `<link>`, all `href` values, logout link |
| `internal/web/templates/*.html` | All `hx-get`, `hx-post`, `hx-delete`, `hx-put`, `action=` values, breadcrumb hrefs |
| Page `<title>` in `base.html` | "TinyDM Admin" → "TinyDM" |
| Root redirect (`/ → /admin/`) | `/ → /app/` |

The static file directory (`internal/web/static/`) does not move — only the URL prefix changes.

---

## Access Control

### `requireAdmin` middleware

Added to `internal/web/web.go`. Reads the `Principal` from context (set by `requireSession`) and checks `UserType == auth.UserTypeAdmin`. If not admin, responds with HTTP 403.

```go
func (h *Handler) requireAdmin(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        p, _ := auth.PrincipalFromContext(r.Context())
        if p.UserType != auth.UserTypeAdmin {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

### Route groups in `RegisterRoutes`

All authenticated routes stay inside the existing `requireSession` group. Admin-only routes are further wrapped in a nested `r.Group` that uses `requireAdmin`:

```go
r.Group(func(r chi.Router) {
    r.Use(h.requireSession)

    // Available to all authenticated users
    r.Get("/app/", h.dashboard)
    r.Get("/app", h.dashboard)
    r.Get("/app/projects", h.projects)
    r.Get("/app/projects/{projectID}/buckets", h.buckets)
    // ... all document routes ...

    // Admin-only
    r.Group(func(r chi.Router) {
        r.Use(h.requireAdmin)
        r.Post("/app/projects", h.createProject)
        r.Delete("/app/projects/{projectID}", h.deleteProject)
        r.Post("/app/projects/{projectID}/buckets", h.createBucket)
        r.Get("/app/projects/{projectID}/buckets/{bucketID}/edit", h.editBucketForm)
        r.Get("/app/projects/{projectID}/buckets/{bucketID}/row", h.bucketRowPartial)
        r.Put("/app/projects/{projectID}/buckets/{bucketID}", h.updateBucket)
        r.Delete("/app/projects/{projectID}/buckets/{bucketID}", h.deleteBucket)
        r.Get("/app/users", h.users)
        r.Post("/app/users", h.createUser)
        // ... all other /users/* routes ...
        r.Get("/app/apikeys", h.apiKeys)
        r.Post("/app/apikeys", h.createAPIKey)
        r.Post("/app/apikeys/{keyID}/revoke", h.revokeAPIKey)
        r.Get("/app/audit", h.auditLog)
        r.Get("/app/audit/events", h.auditEvents)
        // ... all rights panel routes ...
    })
})
```

### Admin-only vs. all-user route breakdown

| Route | Admin only? |
|-------|-------------|
| `GET /app/` | No |
| `GET /app/projects` | No |
| `POST /app/projects` | **Yes** |
| `DELETE /app/projects/{id}` | **Yes** |
| `GET /app/projects/{id}/buckets` | No |
| `POST /app/projects/{id}/buckets` | **Yes** |
| `GET/PUT .../buckets/{id}/edit` | **Yes** |
| `DELETE .../buckets/{id}` | **Yes** |
| All `/app/projects/{id}/buckets/{id}/documents*` | No |
| All `/app/documents/{id}/*` | No |
| All `/app/users/*` | **Yes** |
| All `/app/apikeys/*` | **Yes** |
| All `/app/audit*` | **Yes** |
| All `*/rights*` | **Yes** |

---

## Template Changes

Templates use `{{if eq .Principal.UserType "admin"}}` to gate admin-only UI. `Principal` is already carried in `basePage`, so no new data is required.

### `base.html`

Navigation sidebar:

```html
<!-- Always visible -->
<li><a href="/app/">Dashboard</a></li>
<li><a href="/app/projects">Projects</a></li>

<!-- Admin only -->
{{if eq .Principal.UserType "admin"}}
<li><a href="/app/users">Users</a></li>
<li><a href="/app/apikeys">API Keys</a></li>
<li><a href="/app/audit">Audit Log</a></li>
{{end}}
```

Page title: `TinyDM Admin` → `TinyDM`.

### `dashboard.html`

- Users stat card: wrapped in `{{if eq .Principal.UserType "admin"}}`.
- Recent audit events section: wrapped in `{{if eq .Principal.UserType "admin"}}`.
- Projects, Buckets, Documents stats: visible to all.

### `projects.html`

- "New Project" form/button: `{{if eq .Principal.UserType "admin"}}`.
- Delete button per project row: `{{if eq .Principal.UserType "admin"}}`.
- Rights panel toggle button per row: `{{if eq .Principal.UserType "admin"}}`.

### `buckets.html`

- "New Bucket" form/button: `{{if eq .Principal.UserType "admin"}}`.
- Edit and Delete buttons per bucket row: `{{if eq .Principal.UserType "admin"}}`.
- Rights panel toggle button: `{{if eq .Principal.UserType "admin"}}`.

### `documents.html`

- Upload form: visible to all.
- Delete button per document row: visible to all.
- Rights panel toggle on bucket header: `{{if eq .Principal.UserType "admin"}}`.

### `docdetail.html`

- Edit, delete, upload replacement, tags, properties, version restore: visible to all.
- Rights panel section: `{{if eq .Principal.UserType "admin"}}`.

---

## Affected Files

| File | Change |
|------|--------|
| `internal/web/web.go` | Rename all route paths; add `requireAdmin` middleware; restructure `RegisterRoutes` |
| `internal/web/handlers.go` | Update redirect strings (`/admin/` → `/app/`) |
| `internal/web/templates/base.html` | Rename hrefs, CSS path, title; add nav conditionals |
| `internal/web/templates/dashboard.html` | Hide Users stat + audit section for non-admins |
| `internal/web/templates/projects.html` | Hide create/delete/rights buttons for non-admins |
| `internal/web/templates/buckets.html` | Hide create/edit/delete/rights buttons for non-admins |
| `internal/web/templates/documents.html` | Hide rights panel for non-admins |
| `internal/web/templates/docdetail.html` | Hide rights panel section for non-admins |
| `internal/web/web_test.go` (if exists) | Update path references |
| `internal/web/handlers_test.go` | Update path references |

---

## Testing

- Log in as `admin` — all nav items visible, all actions available.
- Log in as `user` — Users, API Keys, Audit Log nav items absent; create/delete project buttons absent; create/edit/delete bucket buttons absent; rights panels absent.
- Non-admin POSTing directly to `/app/projects` receives HTTP 403.
- Non-admin GET of `/app/users` receives HTTP 403.
- `make test` passes green.
