# General-Purpose UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename the web UI from `/admin/*` to `/app/*`, allow non-admin users to log in, and hide admin-only functionality from `user`-role accounts via server-side middleware and template conditionals.

**Architecture:** A new `requireAdmin` middleware in `web.go` guards all mutating project/bucket routes plus the Users, API Keys, Audit, and Rights routes. Template conditionals using `{{if eq .Principal.UserType "admin"}}` hide the corresponding buttons and nav items. The `/admin/*` prefix is replaced with `/app/*` uniformly across routes, templates, and handlers.

**Tech Stack:** Go 1.21, chi router, Go `html/template`, HTMX 2.

---

## File Map

| File | Change |
|------|--------|
| `internal/web/web.go` | Add `requireAdmin` middleware; rename all routes `/admin/` → `/app/`; restructure `RegisterRoutes` with nested admin group; update `requireSession` redirect |
| `internal/web/handlers.go` | Update two redirect strings (`/admin/` → `/app/`) |
| `internal/web/handlers_test.go` | Add `createNonAdminToken` helper + 3 new tests; update all `/admin/` path literals to `/app/` |
| `internal/web/templates/base.html` | Rename CSS path and all hrefs; update title; admin-conditional nav items |
| `internal/web/templates/login.html` | Rename CSS path and form action; update subtitle |
| `internal/web/templates/projects.html` | Rename all hrefs; admin conditionals for create form, delete/rights buttons, header quick-links |
| `internal/web/templates/buckets.html` | Rename all hrefs; admin conditionals for create form, edit/delete/rights buttons |
| `internal/web/templates/documents.html` | Rename all `/admin/` hrefs to `/app/` (no new conditionals) |
| `internal/web/templates/docdetail.html` | Rename all hrefs; wrap `document-rights-panel` template call in admin conditional |
| `internal/web/templates/dashboard.html` | Rename hrefs; admin conditionals for Users stat card and Recent Activity section |
| `internal/web/templates/users.html` | Rename all `/admin/` hrefs to `/app/` |
| `internal/web/templates/apikeys.html` | Rename all `/admin/` hrefs to `/app/` |
| `internal/web/templates/audit.html` | Rename all `/admin/` hrefs to `/app/` |

---

### Task 1: Write failing tests for the new behaviour

**Files:**
- Modify: `internal/web/handlers_test.go`

- [ ] **Step 1: Add `createNonAdminToken` helper after `sessionReq`**

Open `internal/web/handlers_test.go`. After the closing brace of `sessionReq` (line ~90), add:

```go
// createNonAdminToken seeds a non-admin user in authStore and returns a JWT session token.
func createNonAdminToken(t *testing.T, authStore *auth.Store) string {
	t.Helper()
	hash, _ := auth.HashPassword("userpass")
	user, err := authStore.CreateUser(context.Background(), "bob", "bob@test.local", "Bob", "Jones", hash, auth.UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser non-admin: %v", err)
	}
	token, err := auth.NewJWT(webTestJWTSecret, 60, user.ID, user.Username, user.UserType)
	if err != nil {
		t.Fatalf("NewJWT non-admin: %v", err)
	}
	return token
}
```

- [ ] **Step 2: Add three new tests at the end of the file**

```go
// TestRequireAdmin_NonAdminForbiddenOnCreateProject verifies that a non-admin user
// gets 403 when POSTing to /app/projects.
func TestRequireAdmin_NonAdminForbiddenOnCreateProject(t *testing.T) {
	srv, authStore, _, _, _ := newWebServer(t)
	userToken := createNonAdminToken(t, authStore)

	form := url.Values{"name": {"test-proj"}}
	req := sessionReq(t, http.MethodPost, srv.URL+"/app/projects", userToken,
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /app/projects: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
	}
}

// TestNonAdminCanAccessProjects verifies that a non-admin user gets 200 on GET /app/projects.
func TestNonAdminCanAccessProjects(t *testing.T) {
	srv, authStore, _, _, _ := newWebServer(t)
	userToken := createNonAdminToken(t, authStore)

	req := sessionReq(t, http.MethodGet, srv.URL+"/app/projects", userToken, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /app/projects: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, body)
	}
}

// TestRequireAdmin_NonAdminForbiddenOnUsers verifies that a non-admin user
// gets 403 when accessing the users page.
func TestRequireAdmin_NonAdminForbiddenOnUsers(t *testing.T) {
	srv, authStore, _, _, _ := newWebServer(t)
	userToken := createNonAdminToken(t, authStore)

	req := sessionReq(t, http.MethodGet, srv.URL+"/app/users", userToken, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /app/users: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 3: Run the new tests to verify they fail**

```bash
cd /Users/markdav/dev/tinydm
go test ./internal/web/... -run "TestRequireAdmin|TestNonAdminCan" -v
```

Expected: FAIL — all three tests get 404 (routes don't exist at `/app/` yet).

---

### Task 2: Add `requireAdmin` middleware and rename routes in `web.go`

**Files:**
- Modify: `internal/web/web.go`

- [ ] **Step 1: Add `requireAdmin` middleware**

In `internal/web/web.go`, after the closing brace of `requireSession` (around line 227), add:

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

- [ ] **Step 2: Rewrite `RegisterRoutes` with `/app/` prefix and nested admin group**

Replace the entire `RegisterRoutes` function body with:

```go
func RegisterRoutes(r chi.Router, h *Handler) {
	// Serve embedded static assets.
	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/app/static/*", http.StripPrefix("/app/static/", http.FileServer(http.FS(staticSub))))

	// Login / logout (no session required).
	r.Get("/app/login", h.loginPage)
	r.Post("/app/login", h.loginSubmit)
	r.Get("/app/logout", h.logout)

	// All other app routes require a valid session.
	r.Group(func(r chi.Router) {
		r.Use(h.requireSession)

		// Dashboard — all logged-in users.
		r.Get("/app/", h.dashboard)
		r.Get("/app", h.dashboard)

		// Projects list — all logged-in users.
		r.Get("/app/projects", h.projects)

		// Buckets list — all logged-in users.
		r.Get("/app/projects/{projectID}/buckets", h.buckets)

		// Documents — all logged-in users (full CRUD).
		r.Get("/app/projects/{projectID}/buckets/{bucketID}/documents", h.documents)
		r.Post("/app/projects/{projectID}/buckets/{bucketID}/documents", h.uploadDocument)
		r.Get("/app/projects/{projectID}/buckets/{bucketID}/documents/rows", h.documentRows)

		// Document detail / edit — all logged-in users.
		r.Get("/app/documents/{documentID}", h.documentDetail)
		r.Put("/app/documents/{documentID}", h.updateDocument)
		r.Get("/app/documents/{documentID}/edit", h.editDocumentForm)
		r.Get("/app/documents/{documentID}/row", h.documentRowPartial)
		r.Delete("/app/documents/{documentID}", h.deleteDocument)
		r.Get("/app/documents/{documentID}/download", h.downloadDocument)

		// Document tags — all logged-in users.
		r.Post("/app/documents/{documentID}/tags", h.addDocumentTag)
		r.Delete("/app/documents/{documentID}/tags/{tag}", h.removeDocumentTag)

		// Document properties — all logged-in users.
		r.Post("/app/documents/{documentID}/properties", h.setDocumentPropertyWeb)
		r.Delete("/app/documents/{documentID}/properties/{key}", h.deleteDocumentPropertyWeb)

		// Document version restore — all logged-in users.
		r.Post("/app/documents/{documentID}/versions/{versionID}/restore", h.restoreDocumentVersionWeb)

		// ── Admin-only routes ──────────────────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(h.requireAdmin)

			// Projects — create / delete.
			r.Post("/app/projects", h.createProject)
			r.Delete("/app/projects/{projectID}", h.deleteProject)

			// Buckets — create / edit / delete.
			r.Post("/app/projects/{projectID}/buckets", h.createBucket)
			r.Get("/app/projects/{projectID}/buckets/{bucketID}/edit", h.editBucketForm)
			r.Get("/app/projects/{projectID}/buckets/{bucketID}/row", h.bucketRowPartial)
			r.Put("/app/projects/{projectID}/buckets/{bucketID}", h.updateBucket)
			r.Delete("/app/projects/{projectID}/buckets/{bucketID}", h.deleteBucket)

			// Users.
			r.Get("/app/users", h.users)
			r.Post("/app/users", h.createUser)
			r.Post("/app/users/{userID}/activate", h.activateUser)
			r.Post("/app/users/{userID}/deactivate", h.deactivateUser)
			r.Get("/app/users/{userID}/row", h.userRow)
			r.Get("/app/users/{userID}/password-form", h.passwordForm)
			r.Post("/app/users/{userID}/password", h.changeUserPassword)
			r.Delete("/app/users/{userID}", h.deleteUser)

			// API keys.
			r.Get("/app/apikeys", h.apiKeys)
			r.Post("/app/apikeys", h.createAPIKey)
			r.Post("/app/apikeys/{keyID}/revoke", h.revokeAPIKey)

			// Rights management — by principal.
			r.Get("/app/users/{userID}/rights", h.userRightsPanel)
			r.Post("/app/users/{userID}/rights", h.addUserRight)
			r.Delete("/app/users/{userID}/rights", h.removeUserRight)
			r.Get("/app/apikeys/{keyID}/rights", h.apiKeyRightsPanel)
			r.Post("/app/apikeys/{keyID}/rights", h.addAPIKeyRight)
			r.Delete("/app/apikeys/{keyID}/rights", h.removeAPIKeyRight)

			// Rights management — by resource.
			r.Get("/app/projects/{projectID}/rights", h.projectRightsPanel)
			r.Post("/app/projects/{projectID}/rights", h.addProjectRight)
			r.Delete("/app/projects/{projectID}/rights", h.removeProjectRight)
			r.Get("/app/projects/{projectID}/buckets/{bucketID}/rights", h.bucketRightsPanel)
			r.Post("/app/projects/{projectID}/buckets/{bucketID}/rights", h.addBucketRight)
			r.Delete("/app/projects/{projectID}/buckets/{bucketID}/rights", h.removeBucketRight)
			r.Get("/app/documents/{documentID}/rights", h.documentRightsPanel)
			r.Post("/app/documents/{documentID}/rights", h.addDocumentRight)
			r.Delete("/app/documents/{documentID}/rights", h.removeDocumentRight)

			// Audit log.
			r.Get("/app/audit", h.auditLog)
			r.Get("/app/audit/events", h.auditEvents)
		})
	})

	// Redirect / → /app/
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})
}
```

- [ ] **Step 3: Update `requireSession` redirect from `/admin/login` to `/app/login`**

In the `requireSession` function, find the two occurrences of `"/admin/login"` and change both to `"/app/login"`:

```go
// Line ~206 — unauthenticated redirect:
http.Redirect(w, r, "/app/login", http.StatusFound)

// Line ~211 — invalid JWT redirect:
http.Redirect(w, r, "/app/login", http.StatusFound)

// Line ~216 — inactive/missing user redirect:
http.Redirect(w, r, "/app/login", http.StatusFound)
```

- [ ] **Step 4: Build to check for compilation errors**

```bash
go build ./internal/web/...
```

Expected: exits 0 with no output.

- [ ] **Step 5: Run the three new tests to verify they now pass**

```bash
go test ./internal/web/... -run "TestRequireAdmin|TestNonAdminCan" -v
```

Expected: all three PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/web.go
git commit -m "feat(web): add requireAdmin middleware and rename routes /admin → /app"
```

---

### Task 3: Update existing tests to use `/app/` paths

**Files:**
- Modify: `internal/web/handlers_test.go`

The existing tests still reference `/admin/` paths. After the route rename they return 404. Update every URL literal.

- [ ] **Step 1: Update `TestAuditLog_Renders`**

Change:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/admin/audit", token, nil)
```
To:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/app/audit", token, nil)
```

- [ ] **Step 2: Update `TestAuditLog_GlobalRouteGone`**

Change:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/admin/tenants", token, nil)
```
To:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/app/tenants", token, nil)
```

- [ ] **Step 3: Update `TestPasswordForm_ReturnsInputFragment`**

Change:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/admin/users/"+user.ID+"/password-form", token, nil)
```
To:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/app/users/"+user.ID+"/password-form", token, nil)
```

- [ ] **Step 4: Update `TestPasswordChange_Web_ReturnsUserRow`**

Change:
```go
req := sessionReq(t, http.MethodPost, srv.URL+"/admin/users/"+user.ID+"/password", token,
```
To:
```go
req := sessionReq(t, http.MethodPost, srv.URL+"/app/users/"+user.ID+"/password", token,
```

- [ ] **Step 5: Update `TestAPIKeysPage_Renders`**

Change:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/admin/apikeys", token, nil)
```
To:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/app/apikeys", token, nil)
```

- [ ] **Step 6: Update `TestUsersPage_Renders`**

Change:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/admin/users", token, nil)
```
To:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/app/users", token, nil)
```

- [ ] **Step 7: Update `TestDashboardPage_Renders`**

Change:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/admin/", token, nil)
```
To:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/app/", token, nil)
```

- [ ] **Step 8: Update `TestProjectsPage_Renders`**

Change:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/admin/projects", token, nil)
```
To:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/app/projects", token, nil)
```

- [ ] **Step 9: Update `TestBucketsPage_Renders`**

Change:
```go
req := sessionReq(t, http.MethodGet,
    srv.URL+"/admin/projects/"+project.ID+"/buckets",
    token, nil)
```
To:
```go
req := sessionReq(t, http.MethodGet,
    srv.URL+"/app/projects/"+project.ID+"/buckets",
    token, nil)
```

- [ ] **Step 10: Update `TestDocumentsPage_Renders`**

Change:
```go
req := sessionReq(t, http.MethodGet,
    srv.URL+"/admin/projects/"+project.ID+"/buckets/"+bucket.ID+"/documents",
    token, nil)
```
To:
```go
req := sessionReq(t, http.MethodGet,
    srv.URL+"/app/projects/"+project.ID+"/buckets/"+bucket.ID+"/documents",
    token, nil)
```

- [ ] **Step 11: Update `TestDocumentDetailPage_Renders`**

Change:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/admin/documents/"+doc.ID, token, nil)
```
To:
```go
req := sessionReq(t, http.MethodGet, srv.URL+"/app/documents/"+doc.ID, token, nil)
```

- [ ] **Step 12: Run all web tests**

```bash
go test ./internal/web/... -v
```

Expected: all tests PASS.

- [ ] **Step 13: Commit**

```bash
git add internal/web/handlers_test.go
git commit -m "test(web): update all test paths from /admin to /app, add requireAdmin tests"
```

---

### Task 4: Update redirect strings in `handlers.go`

**Files:**
- Modify: `internal/web/handlers.go`

- [ ] **Step 1: Update `loginSubmit` redirect**

Find (around line 137):
```go
http.Redirect(w, r, "/admin/", http.StatusFound)
```
Change to:
```go
http.Redirect(w, r, "/app/", http.StatusFound)
```

- [ ] **Step 2: Update `logout` redirect**

Find (around line 142):
```go
http.Redirect(w, r, "/admin/login", http.StatusFound)
```
Change to:
```go
http.Redirect(w, r, "/app/login", http.StatusFound)
```

- [ ] **Step 3: Update `deleteDocument` post-delete redirect in `docdetail.html` (it lives in the template, covered in Task 9 — skip here)**

No further changes needed in `handlers.go`.

- [ ] **Step 4: Build and run all tests**

```bash
go build ./... && go test ./internal/web/... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/handlers.go
git commit -m "feat(web): update login/logout redirects from /admin to /app"
```

---

### Task 5: Update `base.html`

**Files:**
- Modify: `internal/web/templates/base.html`

- [ ] **Step 1: Replace the entire file with the updated content**

```html
{{define "base"}}<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{block "title" .}}TinyDM{{end}} — TinyDM</title>
  <link rel="stylesheet" href="/app/static/style.css">
  <script src="https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js" defer></script>
</head>
<body>
<div class="layout">

  <!-- Sidebar -->
  <aside class="sidebar">
    <div class="sidebar-brand">Tiny<span>DM</span></div>

    <p class="sidebar-section">Navigation</p>
    <ul class="sidebar-nav">
      <li><a href="/app/" {{if eq .Page "dashboard"}}class="active"{{end}}>
        <span class="icon">◈</span> Dashboard
      </a></li>
      <li><a href="/app/projects" {{if eq .Page "projects"}}class="active"{{end}}>
        <span class="icon">⬡</span> Projects
      </a></li>
      {{if eq .Principal.UserType "admin"}}
      <li><a href="/app/users" {{if eq .Page "users"}}class="active"{{end}}>
        <span class="icon">◎</span> Users
      </a></li>
      <li><a href="/app/apikeys" {{if eq .Page "apikeys"}}class="active"{{end}}>
        <span class="icon">◇</span> API Keys
      </a></li>
      <li><a href="/app/audit" {{if eq .Page "audit"}}class="active"{{end}}>
        <span class="icon">◉</span> Audit Log
      </a></li>
      {{end}}
    </ul>

    <div class="sidebar-footer">
      <div class="user-info">Signed in as</div>
      <div class="user-name">{{.Principal.Username}}</div>
      <a href="/app/logout" class="btn btn-ghost btn-sm" style="margin-top:8px;width:100%;justify-content:center;">Sign out</a>
    </div>
  </aside>

  <!-- Main -->
  <div class="main">
    <header class="topbar">
      <span class="topbar-title">{{block "title" .}}{{end}}</span>
      <div class="topbar-actions">
        <span class="topbar-user">{{.Principal.Username}}</span>
      </div>
    </header>

    <main class="content" id="main-content">
      {{if .Flash}}<div class="flash flash-ok">{{.Flash}}</div>{{end}}
      {{if .Error}}<div class="flash flash-err">{{.Error}}</div>{{end}}
      <div id="oob-notice"></div>
      {{block "content" .}}{{end}}
    </main>
  </div>

</div>
</body>
</html>
{{end}}

{{/* ── Shared pagination bar ───────────────────────────────────────────────── */}}
{{define "pagination"}}
{{if gt .TotalPages 1}}
<nav class="pagination" aria-label="Pagination">
  <span class="pagination-info">{{.Total}} total · page {{.Page}} of {{.TotalPages}}</span>
  <div class="pagination-links">
    {{if .HasPrev}}
      <a class="btn btn-ghost btn-sm" href="?page={{.PrevPage}}&limit={{.Limit}}{{.ExtraQuery}}">← Prev</a>
    {{else}}
      <span class="btn btn-ghost btn-sm disabled">← Prev</span>
    {{end}}
    {{if .HasNext}}
      <a class="btn btn-ghost btn-sm" href="?page={{.NextPage}}&limit={{.Limit}}{{.ExtraQuery}}">Next →</a>
    {{else}}
      <span class="btn btn-ghost btn-sm disabled">Next →</span>
    {{end}}
  </div>
</nav>
{{end}}
{{end}}
```

- [ ] **Step 2: Run all web tests**

```bash
go test ./internal/web/... -v
```

Expected: all tests PASS (templates are pre-parsed at startup; test server exercises the live templates).

- [ ] **Step 3: Commit**

```bash
git add internal/web/templates/base.html
git commit -m "feat(web): rename /admin→/app in base.html, add admin-conditional nav items"
```

---

### Task 6: Update `login.html`

**Files:**
- Modify: `internal/web/templates/login.html`

- [ ] **Step 1: Replace the entire file**

```html
{{define "login"}}<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Sign in — TinyDM</title>
  <link rel="stylesheet" href="/app/static/style.css">
</head>
<body>
<div class="login-page">
  <div class="login-card">
    <div class="login-logo">Tiny<span>DM</span></div>
    <h1>Sign in</h1>
    <p class="subtitle">Document Management</p>

    {{if .Error}}<div class="flash flash-err">{{.Error}}</div>{{end}}

    <form method="POST" action="/app/login">
      <div class="form-group">
        <label for="username">Username</label>
        <input type="text" id="username" name="username"
               value="{{.Username}}" autocomplete="username" required autofocus>
      </div>
      <div class="form-group">
        <label for="password">Password</label>
        <input type="password" id="password" name="password"
               autocomplete="current-password" required>
      </div>
      <button type="submit" class="btn btn-primary">Sign in</button>
    </form>
  </div>
</div>
</body>
</html>
{{end}}
```

- [ ] **Step 2: Run all web tests**

```bash
go test ./internal/web/... -v
```

Expected: all tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/web/templates/login.html
git commit -m "feat(web): rename /admin→/app in login.html, update subtitle"
```

---

### Task 7: Update `projects.html`

**Files:**
- Modify: `internal/web/templates/projects.html`

- [ ] **Step 1: Replace the entire file**

```html
{{define "title"}}Projects{{end}}
{{define "content"}}

<div class="page-header">
  <div><h1>Projects</h1><p>All projects</p></div>
  {{if eq .Principal.UserType "admin"}}
  <div style="display:flex;gap:8px;">
    <a class="btn btn-ghost btn-sm" href="/app/users">Users</a>
    <a class="btn btn-ghost btn-sm" href="/app/apikeys">API Keys</a>
    <a class="btn btn-ghost btn-sm" href="/app/audit">Audit Log</a>
  </div>
  {{end}}
</div>

<div class="card">
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>Name</th>
          <th>Description</th>
          <th>Buckets</th>
          <th></th>
        </tr>
      </thead>
      <tbody id="projects-list">
        {{range .Projects}}
        {{template "project-row" .}}
        {{end}}
      </tbody>
    </table>
    {{if not .Projects}}
    <div class="empty-state"><p>No projects yet.</p></div>
    {{end}}
  </div>

  {{if eq .Principal.UserType "admin"}}
  <div class="inline-form">
    <div class="form-group">
      <label>Name</label>
      <input type="text" name="name" id="new-proj-name" placeholder="my-project" required>
    </div>
    <div class="form-group">
      <label>Description</label>
      <input type="text" name="description" id="new-proj-desc" placeholder="Optional">
    </div>
    <button class="btn btn-primary"
      hx-post="/app/projects"
      hx-include="#new-proj-name,#new-proj-desc"
      hx-target="#projects-list"
      hx-swap="beforeend"
      hx-on::after-request="this.closest('.inline-form').querySelectorAll('input').forEach(i=>i.value='')">
      + Add Project
    </button>
  </div>
  {{end}}
</div>
{{template "pagination" .Pager}}
<div id="rights-panel-area"></div>
{{end}}

{{define "project-row"}}
<tr id="project-{{.ID}}">
  <td><a href="/app/projects/{{.ID}}/buckets">{{.Name}}</a></td>
  <td class="muted">{{.Description}}</td>
  <td class="muted">—</td>
  <td class="text-right">
    {{if eq $.Principal.UserType "admin"}}
    <button class="btn btn-ghost btn-sm"
      hx-get="/app/projects/{{.ID}}/rights"
      hx-target="#rights-panel-area"
      hx-swap="innerHTML">
      Rights
    </button>
    <button class="btn btn-danger btn-sm"
      hx-delete="/app/projects/{{.ID}}"
      hx-target="#project-{{.ID}}"
      hx-swap="outerHTML"
      hx-confirm="Delete project '{{.Name}}'?">
      Delete
    </button>
    {{end}}
  </td>
</tr>
{{end}}

{{define "project-rights-panel"}}
<div class="card" id="project-rights-panel-{{.ResourceID}}">
  <div style="padding:12px 16px;border-bottom:1px solid var(--border);">
    <strong>Rights — {{.ResourceName}}</strong>
  </div>
  {{if .Rights}}
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>Principal</th>
          <th>Level</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {{range .Rights}}
        <tr>
          <td>
            <span class="badge badge-gray">{{.PrincipalType}}</span>
            {{.PrincipalName}}
          </td>
          <td>{{.PermLevel}}</td>
          <td class="text-right">
            <button class="btn btn-danger btn-sm"
              hx-delete="/app/projects/{{$.ResourceID}}/rights"
              hx-vals='{"principal":"{{.PrincipalType | js}}:{{.PrincipalID | js}}"}'
              hx-target="#project-rights-panel-{{$.ResourceID}}"
              hx-swap="outerHTML">
              Remove
            </button>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
  {{else}}
  <div class="empty-state"><p>No rights granted yet.</p></div>
  {{end}}
  <div class="inline-form" style="padding:12px 16px;border-top:1px solid var(--border);">
    <div class="form-group">
      <label>Principal</label>
      <select name="principal" id="pr-principal-{{.ResourceID}}">
        <optgroup label="Users">
          {{range .Users}}
          <option value="user:{{.ID}}">{{.Username}}</option>
          {{end}}
        </optgroup>
        <optgroup label="API Keys">
          {{range .APIKeys}}
          <option value="apikey:{{.ID}}">{{.Name}} ({{.KeyPrefix}}…)</option>
          {{end}}
        </optgroup>
      </select>
    </div>
    <div class="form-group">
      <label>Level</label>
      <select name="perm_level" id="pr-pl-{{.ResourceID}}">
        <option value="read">Read</option>
        <option value="create">Create</option>
        <option value="update">Update</option>
        <option value="delete">Delete</option>
      </select>
    </div>
    <div class="form-group" style="flex-direction:row;gap:8px;align-items:center;">
      <label><input type="checkbox" name="cascade" id="pr-cas-{{.ResourceID}}"> Cascade to all buckets and documents</label>
    </div>
    <button class="btn btn-primary"
      hx-post="/app/projects/{{.ResourceID}}/rights"
      hx-include="#pr-principal-{{.ResourceID}},#pr-pl-{{.ResourceID}},#pr-cas-{{.ResourceID}}"
      hx-target="#project-rights-panel-{{.ResourceID}}"
      hx-swap="outerHTML">
      Grant
    </button>
  </div>
</div>
{{end}}
```

Note: `project-row` is rendered as a partial with `.` set to a `*repo.Project`. For the admin conditional to work inside `project-row`, the partial must receive the full page data. However, currently `createProject` renders `project-row` with just `*repo.Project` (no Principal). This partial is only triggered by the HTMX "Add Project" button — which is already hidden from non-admins. The admin conditional `{{if eq $.Principal.UserType "admin"}}` inside `project-row` will produce an empty string (rather than panic) when `$.Principal` is the zero value, so this is safe.

- [ ] **Step 2: Run all web tests**

```bash
go test ./internal/web/... -v
```

Expected: all tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/web/templates/projects.html
git commit -m "feat(web): rename /admin→/app in projects.html, add admin conditionals"
```

---

### Task 8: Update `buckets.html`

**Files:**
- Modify: `internal/web/templates/buckets.html`

- [ ] **Step 1: Replace the entire file**

```html
{{define "title"}}Buckets{{end}}
{{define "content"}}
<div class="breadcrumb">
  <a href="/app/projects">Projects</a>
  <span class="sep">›</span>
  <span class="current">{{.Project.Name}}</span>
</div>

<div class="page-header">
  <div><h1>Buckets</h1><p>Project: <strong>{{.Project.Name}}</strong></p></div>
</div>

<div class="stats-grid">
  <div class="stat-card">
    <div class="label">Buckets</div>
    <div class="value">{{.Stats.Buckets}}</div>
  </div>
  <div class="stat-card">
    <div class="label">Documents</div>
    <div class="value">{{.Stats.Documents}}</div>
  </div>
</div>

<div class="card">
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>Name</th>
          <th>Description</th>
          <th>Documents</th>
          <th></th>
        </tr>
      </thead>
      <tbody id="buckets-list">
        {{range .Buckets}}
        {{template "bucket-row" .}}
        {{end}}
      </tbody>
    </table>
    {{if not .Buckets}}
    <div class="empty-state"><p>No buckets yet.</p></div>
    {{end}}
  </div>

  {{if eq .Principal.UserType "admin"}}
  <div class="inline-form">
    <div class="form-group">
      <label>Name</label>
      <input type="text" name="name" id="new-bkt-name" placeholder="assets" required>
    </div>
    <div class="form-group">
      <label>Description</label>
      <input type="text" name="description" id="new-bkt-desc" placeholder="Optional">
    </div>
    <button class="btn btn-primary"
      hx-post="/app/projects/{{.Project.ID}}/buckets"
      hx-include="#new-bkt-name,#new-bkt-desc"
      hx-target="#buckets-list"
      hx-swap="beforeend"
      hx-on::after-request="this.closest('.inline-form').querySelectorAll('input').forEach(i=>i.value='')">
      + Add Bucket
    </button>
  </div>
  {{end}}
</div>
{{template "pagination" .Pager}}
<div id="rights-panel-area"></div>
{{end}}

{{define "bucket-row"}}
<tr id="bucket-{{.ID}}">
  <td><a href="/app/projects/{{.ProjectID}}/buckets/{{.ID}}/documents">{{.Name}}</a></td>
  <td class="muted">{{.Description}}</td>
  <td class="muted">{{.DocCount}}</td>
  <td class="text-right">
    <div class="flex gap-8 items-center" style="justify-content:flex-end;">
      {{if eq $.Principal.UserType "admin"}}
      <button class="btn btn-ghost btn-sm"
        hx-get="/app/projects/{{.ProjectID}}/buckets/{{.ID}}/rights"
        hx-target="#rights-panel-area"
        hx-swap="innerHTML">
        Rights
      </button>
      <button class="btn btn-ghost btn-sm"
        hx-get="/app/projects/{{.ProjectID}}/buckets/{{.ID}}/edit"
        hx-target="#bucket-{{.ID}}"
        hx-swap="outerHTML">
        Edit
      </button>
      <button class="btn btn-danger btn-sm"
        hx-delete="/app/projects/{{.ProjectID}}/buckets/{{.ID}}"
        hx-target="#bucket-{{.ID}}"
        hx-swap="outerHTML"
        hx-confirm="Delete bucket '{{.Name}}'?">
        Delete
      </button>
      {{end}}
    </div>
  </td>
</tr>
{{end}}

{{define "bucket-edit-row"}}
<tr id="bucket-{{.ID}}">
  <td colspan="3">
    <div class="edit-row-form">
      <div class="form-group">
        <label>Name</label>
        <input type="text" id="bkt-name-{{.ID}}" name="name" value="{{.Name}}" required>
      </div>
      <div class="form-group">
        <label>Description</label>
        <input type="text" id="bkt-desc-{{.ID}}" name="description" value="{{.Description}}">
      </div>
      <button class="btn btn-primary btn-sm"
        hx-put="/app/projects/{{.ProjectID}}/buckets/{{.ID}}"
        hx-include="#bkt-name-{{.ID}},#bkt-desc-{{.ID}}"
        hx-target="#bucket-{{.ID}}"
        hx-swap="outerHTML">
        Save
      </button>
      <button class="btn btn-ghost btn-sm"
        hx-get="/app/projects/{{.ProjectID}}/buckets/{{.ID}}/row"
        hx-target="#bucket-{{.ID}}"
        hx-swap="outerHTML">
        Cancel
      </button>
    </div>
  </td>
  <td></td>
</tr>
{{end}}

{{define "bucket-rights-panel"}}
<div class="card" id="bucket-rights-panel-{{.ResourceID}}">
  <div style="padding:12px 16px;border-bottom:1px solid var(--border);">
    <strong>Rights — {{.ResourceName}}</strong>
  </div>
  {{if .Rights}}
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>Principal</th>
          <th>Level</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {{range .Rights}}
        <tr>
          <td>
            <span class="badge badge-gray">{{.PrincipalType}}</span>
            {{.PrincipalName}}
          </td>
          <td>{{.PermLevel}}</td>
          <td class="text-right">
            <button class="btn btn-danger btn-sm"
              hx-delete="/app/projects/{{$.ParentID}}/buckets/{{$.ResourceID}}/rights"
              hx-vals='{"principal":"{{.PrincipalType | js}}:{{.PrincipalID | js}}"}'
              hx-target="#bucket-rights-panel-{{$.ResourceID}}"
              hx-swap="outerHTML">
              Remove
            </button>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
  {{else}}
  <div class="empty-state"><p>No rights granted yet.</p></div>
  {{end}}
  <div class="inline-form" style="padding:12px 16px;border-top:1px solid var(--border);">
    <div class="form-group">
      <label>Principal</label>
      <select name="principal" id="bk-principal-{{.ResourceID}}">
        <optgroup label="Users">
          {{range .Users}}
          <option value="user:{{.ID}}">{{.Username}}</option>
          {{end}}
        </optgroup>
        <optgroup label="API Keys">
          {{range .APIKeys}}
          <option value="apikey:{{.ID}}">{{.Name}} ({{.KeyPrefix}}…)</option>
          {{end}}
        </optgroup>
      </select>
    </div>
    <div class="form-group">
      <label>Level</label>
      <select name="perm_level" id="bk-pl-{{.ResourceID}}">
        <option value="read">Read</option>
        <option value="create">Create</option>
        <option value="update">Update</option>
        <option value="delete">Delete</option>
      </select>
    </div>
    <div class="form-group" style="flex-direction:row;gap:8px;align-items:center;">
      <label><input type="checkbox" name="cascade" id="bk-cas-{{.ResourceID}}"> Cascade to all documents in this bucket</label>
    </div>
    <button class="btn btn-primary"
      hx-post="/app/projects/{{.ParentID}}/buckets/{{.ResourceID}}/rights"
      hx-include="#bk-principal-{{.ResourceID}},#bk-pl-{{.ResourceID}},#bk-cas-{{.ResourceID}}"
      hx-target="#bucket-rights-panel-{{.ResourceID}}"
      hx-swap="outerHTML">
      Grant
    </button>
  </div>
</div>
{{end}}
```

Note: `bucket-row` is a partial rendered both as part of the full page (where `$.Principal` is set) and via HTMX after create/edit operations (where it is rendered as a partial with `bucketRow` data and `$.Principal` is zero-value). The admin buttons are already gated server-side (create/edit/delete bucket routes require admin), so a zero-value Principal in the partial just produces no buttons — correct behaviour.

- [ ] **Step 2: Run all web tests**

```bash
go test ./internal/web/... -v
```

Expected: all tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/web/templates/buckets.html
git commit -m "feat(web): rename /admin→/app in buckets.html, add admin conditionals"
```

---

### Task 9: Update `documents.html`

**Files:**
- Modify: `internal/web/templates/documents.html`

This template has no admin-only UI (all document operations are available to all users). Only the URL prefix changes.

- [ ] **Step 1: Replace every `/admin/` occurrence with `/app/`**

The file has these `/admin/` strings to replace:

| Old | New |
|-----|-----|
| `href="/admin/projects"` | `href="/app/projects"` |
| `href="/admin/projects/{{.Project.ID}}/buckets"` | `href="/app/projects/{{.Project.ID}}/buckets"` |
| `hx-post="/admin/projects/{{.Project.ID}}/buckets/{{.Bucket.ID}}/documents"` | `hx-post="/app/projects/{{.Project.ID}}/buckets/{{.Bucket.ID}}/documents"` |
| `hx-get="/admin/projects/{{.Project.ID}}/buckets/{{.Bucket.ID}}/documents/rows"` (×2) | `hx-get="/app/projects/{{.Project.ID}}/buckets/{{.Bucket.ID}}/documents/rows"` |
| `href="/admin/documents/{{.ID}}"` | `href="/app/documents/{{.ID}}"` |
| `hx-get="/admin/documents/{{.ID}}/edit"` | `hx-get="/app/documents/{{.ID}}/edit"` |
| `href="/admin/documents/{{.ID}}/download"` | `href="/app/documents/{{.ID}}/download"` |
| `hx-delete="/admin/documents/{{.ID}}"` | `hx-delete="/app/documents/{{.ID}}"` |
| `hx-put="/admin/documents/{{.ID}}"` | `hx-put="/app/documents/{{.ID}}"` |
| `hx-get="/admin/documents/{{.ID}}/row"` | `hx-get="/app/documents/{{.ID}}/row"` |

Apply all replacements. The resulting file should contain no remaining `/admin/` strings.

- [ ] **Step 2: Verify no `/admin/` strings remain in the file**

```bash
grep -n "/admin/" internal/web/templates/documents.html
```

Expected: no output.

- [ ] **Step 3: Run all web tests**

```bash
go test ./internal/web/... -v
```

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/web/templates/documents.html
git commit -m "feat(web): rename /admin→/app in documents.html"
```

---

### Task 10: Update `docdetail.html`

**Files:**
- Modify: `internal/web/templates/docdetail.html`

- [ ] **Step 1: Replace every `/admin/` occurrence with `/app/`**

The file has these `/admin/` strings:

| Old | New |
|-----|-----|
| `href="/admin/projects"` | `href="/app/projects"` |
| `href="/admin/projects/{{.Project.ID}}/buckets"` | `href="/app/projects/{{.Project.ID}}/buckets"` |
| `href="/admin/projects/{{.Project.ID}}/buckets/{{.Bucket.ID}}/documents"` | `href="/app/projects/{{.Project.ID}}/buckets/{{.Bucket.ID}}/documents"` |
| `href="/admin/documents/{{.Doc.ID}}/download"` | `href="/app/documents/{{.Doc.ID}}/download"` |
| `hx-delete="/admin/documents/{{.Doc.ID}}"` | `hx-delete="/app/documents/{{.Doc.ID}}"` |
| `window.location='/admin/projects/{{.Project.ID}}/buckets/{{.Bucket.ID}}/documents'` | `window.location='/app/projects/{{.Project.ID}}/buckets/{{.Bucket.ID}}/documents'` |
| `hx-post="/admin/documents/{{.Doc.ID}}/properties"` | `hx-post="/app/documents/{{.Doc.ID}}/properties"` |
| `hx-delete="/admin/documents/{{$.DocID}}/tags/{{.}}"` | `hx-delete="/app/documents/{{$.DocID}}/tags/{{.}}"` |
| `hx-post="/admin/documents/{{.DocID}}/tags"` | `hx-post="/app/documents/{{.DocID}}/tags"` |
| `hx-delete="/admin/documents/{{.DocID}}/properties/{{.Key}}"` | `hx-delete="/app/documents/{{.DocID}}/properties/{{.Key}}"` |
| `hx-post="/admin/documents/{{.DocumentID}}/versions/{{.ID}}/restore"` | `hx-post="/app/documents/{{.DocumentID}}/versions/{{.ID}}/restore"` |
| `hx-delete="/admin/documents/{{$.Doc.ID}}/rights"` | `hx-delete="/app/documents/{{$.Doc.ID}}/rights"` |
| `hx-post="/admin/documents/{{.Doc.ID}}/rights"` | `hx-post="/app/documents/{{.Doc.ID}}/rights"` |

- [ ] **Step 2: Wrap the `document-rights-panel` template call in an admin conditional**

Find (around line 135 in the original file, within `{{define "content"}}`):

```
{{template "document-rights-panel" .}}
```

Replace with:

```
{{if eq .Principal.UserType "admin"}}
{{template "document-rights-panel" .}}
{{end}}
```

- [ ] **Step 3: Verify no `/admin/` strings remain**

```bash
grep -n "/admin/" internal/web/templates/docdetail.html
```

Expected: no output.

- [ ] **Step 4: Run all web tests**

```bash
go test ./internal/web/... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/templates/docdetail.html
git commit -m "feat(web): rename /admin→/app in docdetail.html, hide rights panel for non-admins"
```

---

### Task 11: Update `dashboard.html`

**Files:**
- Modify: `internal/web/templates/dashboard.html`

- [ ] **Step 1: Replace the entire file**

```html
{{define "title"}}Dashboard{{end}}
{{define "content"}}
<div class="page-header">
  <div>
    <h1>Dashboard</h1>
    <p>System overview</p>
  </div>
</div>

<div class="stats-grid">
  {{if eq .Principal.UserType "admin"}}
  <div class="stat-card">
    <div class="label">Users</div>
    <div class="value">{{.Stats.Users}}</div>
  </div>
  {{end}}
  <div class="stat-card">
    <div class="label">Projects</div>
    <div class="value">{{.Stats.Projects}}</div>
  </div>
  <div class="stat-card">
    <div class="label">Buckets</div>
    <div class="value">{{.Stats.Buckets}}</div>
  </div>
  <div class="stat-card">
    <div class="label">Documents</div>
    <div class="value">{{.Stats.Documents}}</div>
  </div>
</div>

{{if eq .Principal.UserType "admin"}}
<div class="card">
  <div class="card-header">
    <h2>Recent Activity</h2>
    <a href="/app/audit" class="btn btn-ghost btn-sm">View all →</a>
  </div>
  <div class="table-wrap">
    {{if .RecentAudit}}
    <table>
      <thead>
        <tr>
          <th>When</th>
          <th>Principal</th>
          <th>Action</th>
          <th>Resource</th>
        </tr>
      </thead>
      <tbody>
        {{range .RecentAudit}}
        <tr>
          <td class="muted">{{.CreatedAt}}</td>
          <td>{{.Principal}}</td>
          <td><span class="badge badge-blue">{{.Action}}</span></td>
          <td class="mono">{{.Resource}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{else}}
    <div class="empty-state">
      <p>No activity yet.</p>
      <p class="hint">Audit events appear here as the API is used.</p>
    </div>
    {{end}}
  </div>
</div>
{{end}}
{{end}}
```

- [ ] **Step 2: Run all web tests**

```bash
go test ./internal/web/... -v
```

Expected: all tests PASS.

- [ ] **Step 3: Run the full test suite**

```bash
go test ./...
```

Expected: all packages PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/web/templates/dashboard.html
git commit -m "feat(web): rename /admin→/app in dashboard.html, hide admin stats for non-admins"
```

---

### Task 12: Update `users.html`, `apikeys.html`, and `audit.html`

**Files:**
- Modify: `internal/web/templates/users.html`
- Modify: `internal/web/templates/apikeys.html`
- Modify: `internal/web/templates/audit.html`

These pages are admin-only (protected by `requireAdmin`), so no conditional guards are needed. Only the URL prefix changes.

- [ ] **Step 1: Replace `/admin/` with `/app/` in `users.html`**

```bash
sed -i '' 's|/admin/|/app/|g' internal/web/templates/users.html
```

Verify:
```bash
grep -n "/admin/" internal/web/templates/users.html
```
Expected: no output.

- [ ] **Step 2: Replace `/admin/` with `/app/` in `apikeys.html`**

```bash
sed -i '' 's|/admin/|/app/|g' internal/web/templates/apikeys.html
```

Verify:
```bash
grep -n "/admin/" internal/web/templates/apikeys.html
```
Expected: no output.

- [ ] **Step 3: Replace `/admin/` with `/app/` in `audit.html`**

```bash
sed -i '' 's|/admin/|/app/|g' internal/web/templates/audit.html
```

Verify:
```bash
grep -n "/admin/" internal/web/templates/audit.html
```
Expected: no output.

- [ ] **Step 4: Run all web tests**

```bash
go test ./internal/web/... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Run the full test suite**

```bash
go test ./...
```

Expected: all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/templates/users.html internal/web/templates/apikeys.html internal/web/templates/audit.html
git commit -m "feat(web): rename /admin→/app in users, apikeys, and audit templates"
```

---

## Final verification

After all tasks are committed, run:

```bash
grep -rn "/admin/" internal/web/
```

Expected: **no output** — no remaining `/admin/` references in the web package.

```bash
go test ./...
```

Expected: all packages PASS green.
