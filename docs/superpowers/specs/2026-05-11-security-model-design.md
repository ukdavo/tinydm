# Security Model Refactoring — Design Spec

## Overview

Extend TinyDM's existing RBAC foundation into a fully enforced, fine-grained permission system. The `rights` table, `Can()` function, and `GetUserRights` store method already exist but are not wired into API routes. This spec covers the schema extensions, store additions, enforcement middleware, and admin UI needed to make the permission system operational.

**Non-goals:** `manage_permissions` delegation — permission management stays restricted to domain admins and superadmins only.

---

## 1. Data Layer

### Migration 006: permission system extensions

**`tenants` table** — add `perm_mode` column:

```sql
ALTER TABLE tenants ADD COLUMN perm_mode TEXT NOT NULL DEFAULT 'explicit'
  CHECK(perm_mode IN ('explicit', 'open', 'inherit'));
```

**`rights` table** — the existing CHECK constraints on `principal_type` and `resource_type` must be widened. SQLite requires a full table recreate to change CHECK constraints:

- `principal_type`: extend from `('user','group')` to `('user','group','apikey')`
- `resource_type`: extend from `('tenant','project','bucket')` to `('tenant','project','bucket','document')`

All existing data is preserved via INSERT … SELECT during the recreate.

### Permission mode semantics

| Value | Meaning |
|---|---|
| `explicit` (default) | No access unless a right is explicitly granted. Absence of a right = denied. |
| `open` | Full access within the tenant unless a right is explicitly absent (opt-down). |
| `inherit` | No access unless granted; grants on a parent resource cascade to all children. |

Superadmins and domain admins always bypass mode — they have full access regardless.

### Inheritance chain

```
tenant → project → bucket → document
```

`Can()` checks the most specific level first. If no right is found at that level and mode is `inherit`, it walks up the chain. For `explicit` mode, only an exact resource ID match or the wildcard `*` counts at each level independently.

---

## 2. Store Layer

New methods on `auth.Store`:

### `GetAPIKeyRights(ctx, tenantID, apiKeyID) ([]Right, error)`

Same query shape as `GetUserRights` but filters `principal_type = 'apikey'` and `principal_id = apiKeyID`. Does not include group membership (API keys are not members of groups).

### `UpsertRight(ctx, params UpsertRightParams) error`

INSERT OR REPLACE into `rights`. Parameters:
- `TenantID`, `PrincipalType`, `PrincipalID`
- `ResourceType`, `ResourceID` (specific UUID or `*`)
- `CanCreate`, `CanRead`, `CanUpdate`, `CanDelete` (bool)

### `DeleteRight(ctx, tenantID, principalType, principalID, resourceType, resourceID) error`

Deletes the specific right row matching the composite key. Returns an error if the row does not exist.

### `ListRights(ctx, tenantID, principalType, principalID) ([]Right, error)`

Returns all rights for a given principal, ordered by resource_type, resource_id. Used by the admin UI rights panel.

### `GetTenantPermMode(ctx, tenantID) (PermMode, error)`

Fetches `perm_mode` for a tenant. Returns `PermModeExplicit` if the tenant does not exist. Called once per request by `TenantCtx` middleware.

### `SetTenantPermMode(ctx, tenantID string, mode PermMode) error`

Updates `perm_mode` on a tenant row. Only callable by domain admins/superadmins (enforced at handler level).

---

## 3. Permission Evaluation

### `PermMode` type

```go
type PermMode string

const (
    PermModeExplicit PermMode = "explicit"
    PermModeOpen     PermMode = "open"
    PermModeInherit  PermMode = "inherit"
)
```

### `ResourceAncestor` type

```go
type ResourceAncestor struct {
    Type ResourceType
    ID   string
}
```

### Updated `Can()` signature

```go
func Can(
    p Principal,
    rights []Right,
    mode PermMode,
    action Action,
    resourceType ResourceType,
    resourceID string,
    ancestors ...ResourceAncestor,
) bool
```

### Evaluation logic

1. `p.IsAdmin()` → return `true` immediately (admins bypass all checks).
2. If `mode == PermModeOpen` → if no right row exists for this principal on this resource (at any level), return `true` (open by default). If a right row does exist, treat its action bits as a filter: only the explicitly granted actions are allowed. This lets admins restrict specific users in an otherwise open tenant without affecting everyone else.
3. Build candidate list: `[(resourceType, resourceID), (resourceType, "*")]` plus, if `mode == PermModeInherit`, each ancestor pair in reverse order (bucket, project, tenant).
4. Iterate rights slice: for each candidate pair, check if any right matches and has the action bit set. Return `true` on first match.
5. Return `false` (no matching right found).

The `ancestors` slice is ordered from nearest to furthest (e.g., bucket before project before tenant). Callers build it from the context objects already loaded by the existing middleware chain.

---

## 4. API Enforcement

### New middleware: `RightsCtx`

Runs after `RequireAuth` and `TenantCtx`. Loads rights for the authenticated principal into context:

- `AuthMethodBearer` / `AuthMethodBasic` → `GetUserRights(ctx, tenantID, principalID)`
- `AuthMethodAPIKey` with a user-tied key → `GetUserRights`
- `AuthMethodAPIKey` with a tenant-scoped key → `GetAPIKeyRights(ctx, tenantID, principalID)`

Stores `[]Right` and `PermMode` (fetched from tenant by `TenantCtx`) in context. One DB hit per request.

### `TenantCtx` extension

Extend the existing `TenantCtx` middleware to also load `perm_mode` from the tenant row and store it in the request context alongside the tenant object.

### New middleware factory: `CanMiddleware`

```go
func CanMiddleware(action Action, resourceType ResourceType, getResourceID func(*http.Request) string, getAncestors func(*http.Request) []ResourceAncestor) func(http.Handler) http.Handler
```

Pulls `[]Right` and `PermMode` from context, calls `Can()`, returns HTTP 403 if denied.

### Route changes

Current `RequireAdmin` guards on project/bucket routes are replaced with `CanMiddleware` for the corresponding action. The existing `RequireSuperAdmin` guards on tenant CRUD remain unchanged.

| Route | Action | Resource |
|---|---|---|
| `GET /tenants/{id}/projects` | `read` | `project` (wildcard) |
| `POST /tenants/{id}/projects` | `create` | `project` (wildcard) |
| `GET /projects/{id}` | `read` | `project` (specific ID) |
| `PUT /projects/{id}` | `update` | `project` (specific ID) |
| `DELETE /projects/{id}` | `delete` | `project` (specific ID) |
| `GET /projects/{id}/buckets` | `read` | `bucket` (wildcard) |
| `POST /projects/{id}/buckets` | `create` | `bucket` (wildcard) |
| `GET /buckets/{id}` | `read` | `bucket` (specific ID) + project ancestor |
| `PUT /buckets/{id}` | `update` | `bucket` (specific ID) + project ancestor |
| `DELETE /buckets/{id}` | `delete` | `bucket` (specific ID) + project ancestor |
| `GET /buckets/{id}/documents` | `read` | `document` (wildcard) + bucket + project ancestors |
| `POST /buckets/{id}/documents` | `create` | `document` (wildcard) + bucket + project ancestors |
| `GET /documents/{id}` | `read` | `document` (specific ID) + bucket + project ancestors |
| `PUT /documents/{id}` | `update` | `document` (specific ID) + bucket + project ancestors |
| `DELETE /documents/{id}` | `delete` | `document` (specific ID) + bucket + project ancestors |

Admins short-circuit all `CanMiddleware` checks via the `p.IsAdmin()` branch in `Can()`.

---

## 5. Web UI

### Tenant settings — permission mode

Add an "Access policy" card to the tenant detail/settings page containing a select element with the three mode options. On change, HTMX POSTs to a new endpoint `PATCH /admin/tenants/{tenantID}/settings` which calls `SetTenantPermMode`. Only visible to domain admins and superadmins.

### User detail page — rights panel

Add a "Rights" card to the existing user detail page (below user profile info). The card contains:

- A table listing all rights for the user: resource type badge (colour-coded: tenant=green, project=purple, bucket=blue, document=amber), resource name, CRUD checkmark columns, parent scope label, Remove button.
- An "Add right" button that expands an inline form:
  - Resource type selector (project / bucket / document)
  - Resource picker: dropdown populated by an HTMX fetch of projects/buckets/documents within the tenant
  - CRUD checkboxes
  - Save / Cancel

HTMX endpoints:
- `GET /admin/tenants/{tenantID}/users/{userID}/rights` — render rights table partial
- `POST /admin/tenants/{tenantID}/users/{userID}/rights` — add right (calls `UpsertRight`); form body: `resource_type`, `resource_id`, `can_create`, `can_read`, `can_update`, `can_delete`
- `DELETE /admin/tenants/{tenantID}/users/{userID}/rights/{rightID}` — remove a specific right row by its `id` primary key (calls `DeleteRight`)

### API key detail page — rights panel

Identical panel to the user rights panel, wired to equivalent endpoints under `/admin/tenants/{tenantID}/apikeys/{keyID}/rights` and `/admin/tenants/{tenantID}/apikeys/{keyID}/rights/{rightID}`.

---

## 6. Error Handling

- `Can()` returning `false` → HTTP 403 `{"error":"permission denied"}`
- Invalid `perm_mode` value in DB → treat as `explicit` (fail-safe)
- Rights load failure → log error, treat principal as having no rights (fail-safe toward denial)
- `UpsertRight` / `DeleteRight` errors → HTTP 500, surfaced to user as flash message in UI

---

## 7. Testing

### Unit tests — `rbac_test.go`

- `Can()` with each `PermMode` × each action × exact match, wildcard match, ancestor match, no match
- `PermModeOpen` opt-down: explicit zero-bit right denies; absent right allows
- Admin principal always returns `true`

### Store tests — `auth/store_test.go`

- `UpsertRight` creates and idempotently updates a right row
- `DeleteRight` removes the row; errors on missing row
- `ListRights` returns all rights for a principal, none for another
- `GetAPIKeyRights` returns only apikey-typed rights
- `GetTenantPermMode` / `SetTenantPermMode` roundtrip

### API integration tests — `api/`

- Regular user denied on project create without grant; allowed after grant
- Bucket read with bucket-level grant (no project grant) — `explicit` mode denies; `inherit` mode allows
- Document read inherits from bucket grant in `inherit` mode
- Admin bypasses all permission checks
- Superadmin bypasses all permission checks

### Web UI tests — `web/`

- Rights panel renders for user with and without rights
- Add right / remove right round-trips via HTMX
- Perm mode selector saves and reloads correctly
