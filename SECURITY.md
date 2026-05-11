# TinyDM — Security Review

> Performed: 2026-05-10 | Scope: auth, input validation, path traversal, upload handling, headers

---

## Findings

### HIGH — Cross-tenant authorisation bypass (RESOLVED — no longer applicable)

**Location:** `internal/auth/middleware.go` `RequireAdmin`, `internal/api/routes.go`

**Description:** `RequireAdmin` only checked `UserType == admin`, not that the authenticated
principal's tenant matched the `{tenantID}` URL parameter. An administrator of Tenant A could
access `/api/v1/tenants/tenant-B/users`, `/api/v1/tenants/tenant-B/projects`, and all
sub-resources of Tenant B without restriction.

**Resolution:** Multi-tenancy has been removed from TinyDM. Routes are no longer nested under
`/tenants/{tenantID}`, `principal.TenantID` no longer exists, and the `RequireSameTenant`
middleware (`internal/api/security.go`) was removed as part of the tenant removal — the concept it
protected no longer exists. The vulnerability cannot be reproduced.

---

### HIGH — Unbounded file upload size (FIXED)

**Location:** `internal/api/documents.go` `Upload`, `Update`

**Description:** `ParseMultipartForm(32 << 20)` limits in-memory buffering to 32 MiB but does not
cap the total request body size. An attacker could exhaust disk space by uploading an arbitrarily
large file.

**Fix:** `r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)` (512 MiB) is now applied before
`ParseMultipartForm` in both `Upload` and `Update`. The limit is defined as `maxUploadBytes` in
`internal/api/security.go` and can be adjusted for the deployment's storage budget.

---

### MEDIUM — Client-supplied Content-Type trusted verbatim (FIXED)

**Location:** `internal/api/documents.go` `Upload`, `Update`

**Description:** If the multipart `Content-Type` header was anything other than
`application/octet-stream`, it was accepted as the document's MIME type. A client could claim a
binary executable was `application/pdf`, potentially misleading downstream consumers or triggering
incorrect metadata extraction.

**Fix:** Removed the client-supplied Content-Type override in both handlers. `http.DetectContentType`
(magic-byte inspection) is now the sole source of truth for MIME type.

---

### MEDIUM — Unsanitized filenames (FIXED)

**Location:** `internal/api/documents.go` `Upload`

**Description:** The filename from `header.Filename` was stored in the database verbatim. Values
such as `../../etc/passwd` (path traversal in display name), filenames containing null bytes, and
filenames longer than 255 characters were accepted. While the name is not used as a filesystem path,
it is echoed back in `Content-Disposition` headers and displayed in the admin UI.

**Fix:** Added `sanitizeFilename()` in `internal/api/security.go` which applies `filepath.Base`
(strips directory components), replaces control characters and null bytes with underscores, caps
length at 255 bytes, and rejects reserved names (`.`, `..`).

---

### MEDIUM — Missing security response headers (FIXED)

**Location:** `cmd/tinydm/main.go`

**Description:** No `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`,
`Permissions-Policy`, or `Content-Security-Policy` headers were set, leaving browsers without
standard defence-in-depth protections.

**Fix:** Added `SecurityHeaders` middleware in `internal/api/security.go` and wired it globally in
`main.go`. Sets:

| Header | Value |
|--------|-------|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Permissions-Policy` | camera, microphone, geolocation all disabled |
| `Content-Security-Policy` | `default-src 'self'` with narrow exceptions |

---

### MEDIUM — Session cookie missing `Secure` and `SameSite` flags (FIXED)

**Location:** `internal/web/handlers.go`

**Description:** The admin UI session cookie had `HttpOnly: true` but lacked `Secure` (cookie
transmitted over plain HTTP) and `SameSite` (no CSRF protection at the cookie level).

**Fix:** Added `SameSite: http.SameSiteLaxMode` unconditionally. Added `Secure: h.cfg.SecureCookies`
controlled by the `TINYDM_SECURE_COOKIES=true` environment variable — set this when TinyDM is served
behind a TLS-terminating reverse proxy or with TLS configured directly.

---

### MEDIUM — No JSON body size limit (FIXED)

**Location:** `internal/api/handler.go` `decode()`

**Description:** Non-multipart JSON request bodies (login, property set, etc.) had no
size limit. A client could send a multi-GB JSON body, exhausting server memory.

**Fix:** `decode()` now wraps `r.Body` with `io.LimitReader(r.Body, maxJSONBytes)` (1 MiB) before
decoding. Defined as `maxJSONBytes` in `internal/api/security.go`.

---

## Acknowledged / Won't Fix (this release)

### Login brute-force — no rate limiting

**Severity:** Medium | **Status:** Acknowledged

No rate limiting is applied to `POST /api/v1/auth/login`. Implementing in-process rate limiting
requires shared state across goroutines and becomes incorrect behind multiple replicas. The
recommended mitigation is to deploy TinyDM behind a reverse proxy (nginx, Caddy, Traefik) with
rate limiting configured at that layer. bcrypt cost 12 (used for password hashing) already imposes
~300 ms per check as a natural throttle.

### No CORS policy

**Severity:** Low | **Status:** Accepted

TinyDM's REST API is intended for server-to-server use and its admin UI is server-rendered. No CORS
headers are set, which defaults browsers to same-origin restriction. If the API is called from a
browser-based SPA in a different origin, add an explicit CORS middleware with an allowlist.

### Storage path traversal — not exploitable

**Severity:** Info | **Status:** Accepted

`storage.Get` and `storage.Delete` construct file paths with `filepath.Join(basePath, key)`. The
`key` is always a SHA-256 hex digest sourced from the database, so it only ever contains hex
characters (`[0-9a-f/]`) and cannot traverse the storage root. The protection is implicit rather
than explicit; a future change that exposes the `key` parameter to user input would reintroduce the
risk.

### Audit log is best-effort

**Severity:** Info | **Status:** Accepted by design

Failed audit writes are logged at `WARN` level but do not surface to the API caller. This is
intentional — an audit store failure must never block a legitimate document operation. Monitor the
`audit: failed to record event` log line in production.

---

## Environment variables added

| Variable | Default | Purpose |
|----------|---------|---------|
| `TINYDM_SECURE_COOKIES` | `false` | Set `true` to add the `Secure` flag to the session cookie |
