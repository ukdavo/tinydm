# Remove Tenants — Design Spec

**Date:** 2026-05-11  
**Status:** Approved  
**Scope:** Remove the multi-tenancy concept entirely; make projects the top-level entity.

---

## Background

TinyDM was built with a multi-tenant data hierarchy: Tenant → Project → Bucket → Document. Each tenant had its own users, groups, API keys, RBAC rights, and audit log. The system was designed for a superadmin to provision tenants, each with their own domain admin.

In practice, TinyDM is deployed for a single organisation. The tenant layer adds complexity with no benefit: nested routes, a 3-role model, per-tenant RBAC modes, and a login form that asks for a tenant name. This spec describes removing the concept entirely.

---

## Goals

- Remove the `tenants` table and all `tenant_id` foreign keys from the schema.
- Flatten the data hierarchy to: **Project → Bucket → Document**.
- Simplify the auth model from 3 roles (superadmin/admin/user) to 2 roles (admin/user).
- Flatten API routes from `/api/v1/tenants/{tenantID}/projects/...` to `/api/v1/projects/...`.
- Remove the tenant name field from the login form.
- Keep the RBAC rights system with `project` as the new top-level resource type.

## Non-Goals

- No data migration required — this is a clean-slate change.
- Do not simplify the Project → Bucket → Document hierarchy further.
- Do not remove the RBAC rights system.

---

## Data Model

### Schema changes (new migration)

- Drop the `tenants` table.
- Drop `tenant_id` column from: `projects`, `users`, `groups`, `api_keys`, `rights`, `audit_log`.
- Add a global `UNIQUE` constraint on `users.username` (was previously unique per tenant).
- Remove `resource_type = 'tenant'` as a valid value in the rights system; keep `project`, `bucket`, `document`.
- Remove the `perm_mode` column (was per-tenant RBAC mode). Replace with a system-wide env var `TINYDM_PERM_MODE` (values: `explicit` | `open`; default: `open`). The `inherit` value is removed — without a tenant hierarchy there is nothing to inherit from.

### Bootstrap

Remove env vars:
- `TINYDM_BOOTSTRAP_TENANT_ID`
- `TINYDM_BOOTSTRAP_TENANT_NAME`

Keep:
- `TINYDM_BOOTSTRAP_ADMIN_PASS`

On startup, create the admin user directly — no tenant to create first.

### SQL queries

All queries currently filtering by `tenant_id` are rewritten to remove that filter. The `tenants.sql` query file is deleted. Approximately 40+ queries across `users.sql`, `projects.sql`, `buckets.sql`, `documents.sql`, `groups.sql`, `apikeys.sql`, `rights.sql`, `audit.sql` are updated.

---

## Auth Model

### Roles

| Role | Before | After |
|------|--------|-------|
| `superadmin` | Global admin, manages tenants | **Removed** |
| `admin` | Domain admin, manages one tenant | Full system admin |
| `user` | Document user within a tenant | Document user, RBAC-controlled |

**Deleted:** `UserTypeSuperAdmin` constant, `IsSuperAdmin()` predicate, `RequireSuperAdmin` middleware.

### Principal struct

Remove `TenantID` field:

```go
// Before
type Principal struct {
    ID         string
    TenantID   string
    Username   string
    UserType   UserType
    AuthMethod AuthMethod
}

// After
type Principal struct {
    ID         string
    Username   string
    UserType   UserType
    AuthMethod AuthMethod
}
```

### Authentication methods

**Basic auth:** Drop the `X-Tenant-ID` header requirement. Login is `Authorization: Basic base64(username:password)`. Username lookup becomes a global query against `users.username`.

**JWT:** Remove `tenantID` claim from token payload and validation.

**API keys:** Remove `tenant_id` association. API keys are global credentials.

### Middleware changes

| Middleware | Change |
|------------|--------|
| `RequireSameTenant` | Deleted |
| `TenantCtx` | Deleted |
| `RequireSuperAdmin` | Deleted |
| `RequireAdmin` | Kept — enforces `admin` role |
| `RightsCtx` | Kept — simplified, no tenant-scoped lookup |

---

## API Routes

`internal/api/tenants.go` is deleted. All other handlers are mounted directly under `/api/v1/`.

| Before | After |
|--------|-------|
| `GET /api/v1/tenants` | *(removed)* |
| `POST /api/v1/tenants` | *(removed)* |
| `GET/PUT/DELETE /api/v1/tenants/{tenantID}` | *(removed)* |
| `/api/v1/tenants/{tenantID}/projects` | `/api/v1/projects` |
| `/api/v1/tenants/{tenantID}/projects/{id}/buckets` | `/api/v1/projects/{id}/buckets` |
| `/api/v1/tenants/{tenantID}/projects/{id}/buckets/{bid}/documents` | `/api/v1/projects/{id}/buckets/{bid}/documents` |
| `/api/v1/tenants/{tenantID}/audit` | `/api/v1/audit` |
| `/api/v1/tenants/{tenantID}/users` | `/api/v1/users` |
| `/api/v1/tenants/{tenantID}/groups` | `/api/v1/groups` |
| `/api/v1/tenants/{tenantID}/apikeys` | `/api/v1/apikeys` |
| `/api/v1/tenants/{tenantID}/rights` | `/api/v1/rights` |

`internal/api/routes.go` is rewritten to mount all routes from `/api/v1/` directly with no `{tenantID}` path segment.

---

## Web Admin UI

### Removed

- `/admin/tenants` list/create page, handler, and `tenants.html` template.
- `/admin/tenants/{tenantID}` project dashboard handler.
- Tenant name field on the login form (`/admin/login`).

### Updated

- **Dashboard** (`/admin/`): shows system-wide stats (total projects, buckets, documents, users, recent audit events). No longer restricted to superadmin — any `admin` can access it.
- **Navigation**: tenants link removed; projects are the top-level entry point.
- **All web routes** flatten from `/admin/tenants/{tenantID}/projects/...` to `/admin/projects/...`, `/admin/users/...`, etc.
- **Breadcrumbs**: tenant level removed throughout.

---

## Affected Files

| File | Change |
|------|--------|
| `internal/db/migrations/` | New migration to drop tenant table and columns |
| `internal/db/migrations_pg/` | Same migration for PostgreSQL |
| `internal/db/queries/tenants.sql` | Deleted |
| `internal/db/queries/*.sql` | Remove `tenant_id` filters from all queries |
| `internal/db/` (sqlc-generated) | Regenerate with `make sqlc` |
| `internal/config/config.go` | Remove bootstrap tenant vars; add `TINYDM_PERM_MODE` |
| `internal/auth/context.go` | Remove `TenantID`, `UserTypeSuperAdmin`, `IsSuperAdmin()` |
| `internal/auth/middleware.go` | Remove `RequireSuperAdmin`, `verifyBasic` X-Tenant-ID, JWT tenant claim |
| `internal/auth/rbac.go` | Remove tenant resource type from `Can()` |
| `internal/auth/store.go` | Remove tenant-scoped user queries; delete `CreateDomainAdmin()`; make username lookup global |
| `internal/api/tenants.go` | Deleted |
| `internal/api/routes.go` | Flatten all routes; remove `TenantCtx`, `RequireSameTenant` |
| `internal/api/context.go` | Delete `TenantCtx()`; simplify `RightsCtx()` |
| `internal/api/security.go` | Delete `RequireSameTenant()` |
| `internal/web/handlers.go` | Remove tenant handlers; update login, dashboard, nav |
| `internal/web/templates/login.html` | Remove tenant name input |
| `internal/web/templates/tenants.html` | Deleted |
| `internal/web/templates/*.html` | Update breadcrumbs and nav links |
| `cmd/tinydm/main.go` | Update bootstrap; remove tenant creation |
| `internal/api/*_test.go` | Remove tenant isolation tests; update all test setup |
| `docs/openapi.yaml` (embedded) | Update all paths and schemas to remove tenant scope |
| `README.md`, `DEPLOYMENT.md` | Update API examples and setup instructions |

---

## Testing

- `internal/api/tenants_test.go` — deleted.
- All remaining test helpers that create a test tenant are updated to bootstrap directly with a project.
- `internal/api/security_test.go` — remove cross-tenant tests; keep RBAC grant/deny tests.
- Integration shell scripts (`test_phase*.sh`) updated to use flattened routes.
- `make test` must pass green after all changes.
