# Remove Tenants Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the multi-tenant concept entirely, flattening the hierarchy from Tenant → Project → Bucket → Document to Project → Bucket → Document.

**Architecture:** Drop the `tenants` table and all `tenant_id` foreign keys via a new migration; regenerate sqlc; update the auth, api, web, and config packages to remove all tenant-scoped operations; flatten API routes from `/api/v1/tenants/{tenantID}/...` to `/api/v1/...`.

**Tech Stack:** Go 1.21, Chi router, SQLite (modernc), PostgreSQL (optional), sqlc, HTMX templates, goose migrations.

**Spec:** `docs/superpowers/specs/2026-05-11-remove-tenants-design.md`

---

## File Map

| File | Action |
|------|--------|
| `internal/db/migrations/007_remove_tenants.sql` | Create |
| `internal/db/migrations_pg/006_remove_tenants.sql` | Create |
| `internal/db/queries/tenants.sql` | Delete |
| `internal/db/queries/projects.sql` | Modify — remove tenant_id |
| `internal/db/queries/users.sql` | Modify — remove tenant_id |
| `internal/db/queries/groups.sql` | Modify — remove tenant_id |
| `internal/db/queries/api_keys.sql` | Modify — remove tenant_id |
| `internal/db/queries/rights.sql` | Modify — remove tenant_id |
| `internal/db/queries/audit.sql` | Modify — remove tenant_id |
| `internal/auth/context.go` | Modify — remove TenantID, UserTypeSuperAdmin, IsSuperAdmin |
| `internal/auth/rbac.go` | Modify — remove ResourceTenant, PermModeInherit |
| `internal/auth/token.go` | Modify — remove TenantID from Claims/NewJWT |
| `internal/auth/store.go` | Modify — remove all tenant-scoped raw SQL |
| `internal/auth/middleware.go` | Modify — remove RequireSuperAdmin, X-Tenant-ID |
| `internal/config/config.go` | Modify — remove bootstrap tenant vars, add PermMode |
| `internal/api/auth.go` | Modify — remove tenant_id from login/me |
| `internal/api/context.go` | Modify — delete TenantCtx, simplify RightsCtx/CanMiddleware |
| `internal/api/security.go` | Modify — delete RequireSameTenant |
| `internal/api/users.go` | Modify — remove tenantFromCtx usage |
| `internal/api/tenants.go` | Delete |
| `internal/api/tenants_test.go` | Delete |
| `internal/api/routes.go` | Rewrite — flatten routes |
| `internal/api/server_test.go` | Modify — remove tenant from test helpers |
| `internal/api/auth_test.go` | Modify — remove tenant_id from login requests |
| `cmd/tinydm/main.go` | Modify — simplified bootstrap |
| `internal/web/web.go` | Modify — flatten web routes |
| `internal/web/handlers.go` | Modify — remove tenant handlers, update login/dashboard |
| `internal/web/templates/login.html` | Modify — remove tenant field |
| `internal/web/templates/tenants.html` | Delete |
| `internal/web/templates/base.html` | Modify — remove tenants nav link |

---

## Task 1: SQLite schema migration

**Files:**
- Create: `internal/db/migrations/007_remove_tenants.sql`

- [ ] **Step 1: Create the SQLite migration**

```sql
-- +goose Up
-- 007_remove_tenants.sql
--
-- Removes multi-tenancy: drops the tenants table and tenant_id columns from
-- all dependent tables. Uses the copy-rename pattern for tables whose UNIQUE
-- constraints change. Requires SQLite 3.35+ for ALTER TABLE ... DROP COLUMN.

-- 1. Recreate projects without tenant_id (UNIQUE changes from (tenant_id,name) to (name))
CREATE TABLE projects_v2 (
    id          TEXT     NOT NULL PRIMARY KEY,
    name        TEXT     NOT NULL UNIQUE,
    description TEXT     NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  DATETIME
);
INSERT INTO projects_v2 (id, name, description, created_at, updated_at, deleted_at)
    SELECT id, name, description, created_at, updated_at, deleted_at FROM projects;
DROP TABLE projects;
ALTER TABLE projects_v2 RENAME TO projects;

-- 2. Recreate users without tenant_id; downgrade superadmin → admin; UNIQUE on username globally
CREATE TABLE users_v2 (
    id            TEXT     NOT NULL PRIMARY KEY,
    username      TEXT     NOT NULL UNIQUE,
    email         TEXT     NOT NULL DEFAULT '',
    password_hash TEXT     NOT NULL DEFAULT '',
    user_type     TEXT     NOT NULL DEFAULT 'user'
                           CHECK(user_type IN ('admin','user')),
    is_active     INTEGER  NOT NULL DEFAULT 1,
    first_name    TEXT     NOT NULL DEFAULT '',
    last_name     TEXT     NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at    DATETIME
);
INSERT INTO users_v2 (id, username, email, password_hash, user_type, is_active,
                      first_name, last_name, created_at, updated_at, deleted_at)
    SELECT id, username, email, password_hash,
           CASE user_type WHEN 'superadmin' THEN 'admin' ELSE user_type END,
           is_active,
           COALESCE(first_name, ''), COALESCE(last_name, ''),
           created_at, updated_at, deleted_at
    FROM users;
DROP TABLE users;
ALTER TABLE users_v2 RENAME TO users;

-- 3. Recreate groups without tenant_id
CREATE TABLE groups_v2 (
    id          TEXT     NOT NULL PRIMARY KEY,
    name        TEXT     NOT NULL UNIQUE,
    description TEXT     NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at  DATETIME
);
INSERT INTO groups_v2 (id, name, description, created_at, updated_at, deleted_at)
    SELECT id, name, description, created_at, updated_at, deleted_at FROM groups;
DROP TABLE groups;
ALTER TABLE groups_v2 RENAME TO groups;

-- Recreate group_members FK (references new groups table)
CREATE TABLE group_members_v2 (
    group_id   TEXT     NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id    TEXT     NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, user_id)
);
INSERT INTO group_members_v2 SELECT * FROM group_members;
DROP TABLE group_members;
ALTER TABLE group_members_v2 RENAME TO group_members;
CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_id);

-- 4. Recreate api_keys without tenant_id
CREATE TABLE api_keys_v2 (
    id           TEXT     NOT NULL PRIMARY KEY,
    user_id      TEXT     REFERENCES users(id) ON DELETE SET NULL,
    name         TEXT     NOT NULL,
    key_hash     TEXT     NOT NULL UNIQUE,
    key_prefix   TEXT     NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at   DATETIME,
    last_used_at DATETIME,
    revoked_at   DATETIME
);
INSERT INTO api_keys_v2 (id, user_id, name, key_hash, key_prefix,
                         created_at, expires_at, last_used_at, revoked_at)
    SELECT id, user_id, name, key_hash, key_prefix,
           created_at, expires_at, last_used_at, revoked_at FROM api_keys;
DROP TABLE api_keys;
ALTER TABLE api_keys_v2 RENAME TO api_keys;
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);

-- 5. Recreate rights without tenant_id; remove 'tenant' from resource_type CHECK
CREATE TABLE rights_v2 (
    id             TEXT     NOT NULL PRIMARY KEY,
    principal_type TEXT     NOT NULL CHECK(principal_type IN ('user','group','apikey')),
    principal_id   TEXT     NOT NULL,
    resource_type  TEXT     NOT NULL CHECK(resource_type IN ('project','bucket','document')),
    resource_id    TEXT     NOT NULL DEFAULT '*',
    can_create     INTEGER  NOT NULL DEFAULT 0,
    can_read       INTEGER  NOT NULL DEFAULT 0,
    can_update     INTEGER  NOT NULL DEFAULT 0,
    can_delete     INTEGER  NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(principal_type, principal_id, resource_type, resource_id)
);
INSERT INTO rights_v2 (id, principal_type, principal_id, resource_type, resource_id,
                       can_create, can_read, can_update, can_delete, created_at)
    SELECT id, principal_type, principal_id, resource_type, resource_id,
           can_create, can_read, can_update, can_delete, created_at
    FROM rights WHERE resource_type != 'tenant';
DROP TABLE rights;
ALTER TABLE rights_v2 RENAME TO rights;
CREATE INDEX IF NOT EXISTS idx_rights_principal ON rights(principal_type, principal_id);

-- 6. Drop tenant_id from audit_log (SQLite 3.35+)
ALTER TABLE audit_log DROP COLUMN tenant_id;

-- 7. Drop the tenants table
DROP TABLE IF EXISTS tenants;

-- +goose Down
SELECT 1; -- irreversible; roll forward instead
```

- [ ] **Step 2: Verify the migration file exists**

```bash
ls internal/db/migrations/007_remove_tenants.sql
```

Expected: file listed with no error.

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/007_remove_tenants.sql
git commit -m "feat(db): add SQLite migration 007 to remove tenants"
```

---

## Task 2: PostgreSQL schema migration

**Files:**
- Create: `internal/db/migrations_pg/006_remove_tenants.sql`

- [ ] **Step 1: Create the PostgreSQL migration**

```sql
-- +goose Up
-- 006_remove_tenants.sql (PostgreSQL)
--
-- Removes multi-tenancy: drops the tenants table and tenant_id columns.
-- PostgreSQL supports ALTER TABLE ... DROP COLUMN and DROP CONSTRAINT directly.

-- 1. Projects: drop tenant_id, replace unique constraint
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_tenant_id_name_key;
DROP INDEX IF EXISTS idx_projects_tenant;
ALTER TABLE projects DROP COLUMN tenant_id;
ALTER TABLE projects ADD CONSTRAINT projects_name_key UNIQUE (name);

-- 2. Users: drop tenant_id, downgrade superadmin → admin, replace unique constraints
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_tenant_id_username_key;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_tenant_id_email_key;
DROP INDEX IF EXISTS idx_users_tenant;
ALTER TABLE users DROP COLUMN tenant_id;
UPDATE users SET user_type = 'admin' WHERE user_type = 'superadmin';
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_user_type_check;
ALTER TABLE users ADD CONSTRAINT users_user_type_check CHECK (user_type IN ('admin','user'));
ALTER TABLE users ADD CONSTRAINT users_username_key UNIQUE (username);

-- 3. Groups: drop tenant_id, replace unique constraint
ALTER TABLE groups DROP CONSTRAINT IF EXISTS groups_tenant_id_name_key;
DROP INDEX IF EXISTS idx_groups_tenant;
ALTER TABLE groups DROP COLUMN tenant_id;
ALTER TABLE groups ADD CONSTRAINT groups_name_key UNIQUE (name);

-- 4. API keys: drop tenant_id
DROP INDEX IF EXISTS idx_api_keys_tenant;
ALTER TABLE api_keys DROP COLUMN tenant_id;

-- 5. Rights: drop tenant_id, remove 'tenant' resource_type, tighten CHECK
DROP INDEX IF EXISTS idx_rights_tenant;
ALTER TABLE rights DROP COLUMN tenant_id;
DELETE FROM rights WHERE resource_type = 'tenant';
ALTER TABLE rights DROP CONSTRAINT IF EXISTS rights_resource_type_check;
ALTER TABLE rights ADD CONSTRAINT rights_resource_type_check
    CHECK (resource_type IN ('project','bucket','document'));

-- 6. Audit log: drop tenant_id
DROP INDEX IF EXISTS idx_audit_tenant;
ALTER TABLE audit_log DROP COLUMN tenant_id;

-- 7. Drop tenants table (no FKs remain)
DROP TABLE IF EXISTS tenants;

-- +goose Down
SELECT 1; -- irreversible; roll forward instead
```

- [ ] **Step 2: Commit**

```bash
git add internal/db/migrations_pg/006_remove_tenants.sql
git commit -m "feat(db): add PostgreSQL migration 006 to remove tenants"
```

---

## Task 3: Update SQL query files and regenerate sqlc

**Files:**
- Delete: `internal/db/queries/tenants.sql`
- Modify: `internal/db/queries/projects.sql`
- Modify: `internal/db/queries/users.sql`
- Modify: `internal/db/queries/groups.sql`
- Modify: `internal/db/queries/api_keys.sql`
- Modify: `internal/db/queries/rights.sql`
- Modify: `internal/db/queries/audit.sql`

- [ ] **Step 1: Delete tenants.sql**

```bash
rm internal/db/queries/tenants.sql
```

- [ ] **Step 2: Replace projects.sql**

```sql
-- name: CreateProject :one
INSERT INTO projects (id, name, description)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects
WHERE id = ? AND deleted_at IS NULL;

-- name: GetProjectByName :one
SELECT * FROM projects
WHERE name = ? AND deleted_at IS NULL;

-- name: ListProjects :many
SELECT * FROM projects
WHERE deleted_at IS NULL
ORDER BY name;

-- name: UpdateProject :one
UPDATE projects
SET name        = ?,
    description = ?,
    updated_at  = CURRENT_TIMESTAMP
WHERE id = ? AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteProject :exec
UPDATE projects
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = ?;
```

- [ ] **Step 3: Replace users.sql**

```sql
-- name: CreateUser :one
INSERT INTO users (id, username, email, password_hash, user_type)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = ? AND deleted_at IS NULL;

-- name: GetUserByUsername :one
SELECT * FROM users
WHERE username = ? AND deleted_at IS NULL;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = ? AND deleted_at IS NULL;

-- name: ListUsers :many
SELECT * FROM users
WHERE deleted_at IS NULL
ORDER BY username;

-- name: UpdateUser :one
UPDATE users
SET username   = ?,
    email      = ?,
    user_type  = ?,
    is_active  = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND deleted_at IS NULL
RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = ?,
    updated_at    = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SoftDeleteUser :exec
UPDATE users SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: CountUsers :one
SELECT COUNT(*) FROM users WHERE deleted_at IS NULL;
```

- [ ] **Step 4: Replace groups.sql**

```sql
-- name: CreateGroup :one
INSERT INTO groups (id, name, description)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetGroup :one
SELECT * FROM groups WHERE id = ? AND deleted_at IS NULL;

-- name: GetGroupByName :one
SELECT * FROM groups
WHERE name = ? AND deleted_at IS NULL;

-- name: ListGroups :many
SELECT * FROM groups
WHERE deleted_at IS NULL
ORDER BY name;

-- name: UpdateGroup :one
UPDATE groups
SET name        = ?,
    description = ?,
    updated_at  = CURRENT_TIMESTAMP
WHERE id = ? AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteGroup :exec
UPDATE groups SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: AddGroupMember :exec
INSERT OR IGNORE INTO group_members (group_id, user_id) VALUES (?, ?);

-- name: RemoveGroupMember :exec
DELETE FROM group_members WHERE group_id = ? AND user_id = ?;

-- name: ListGroupMembers :many
SELECT u.* FROM users u
JOIN group_members m ON m.user_id = u.id
WHERE m.group_id = ? AND u.deleted_at IS NULL
ORDER BY u.username;

-- name: ListUserGroups :many
SELECT g.* FROM groups g
JOIN group_members m ON m.group_id = g.id
WHERE m.user_id = ? AND g.deleted_at IS NULL
ORDER BY g.name;
```

- [ ] **Step 5: Replace api_keys.sql**

```sql
-- name: CreateAPIKey :one
INSERT INTO api_keys (id, user_id, name, key_hash, key_prefix, expires_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAPIKey :one
SELECT * FROM api_keys WHERE id = ?;

-- name: GetAPIKeyByHash :one
SELECT * FROM api_keys WHERE key_hash = ?;

-- name: ListAPIKeys :many
SELECT * FROM api_keys
WHERE revoked_at IS NULL
ORDER BY created_at DESC;

-- name: RevokeAPIKey :exec
UPDATE api_keys SET revoked_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?;
```

- [ ] **Step 6: Replace rights.sql**

```sql
-- name: GrantRights :one
INSERT INTO rights (id, principal_type, principal_id,
                    resource_type, resource_id,
                    can_create, can_read, can_update, can_delete)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(principal_type, principal_id, resource_type, resource_id)
DO UPDATE SET
    can_create = excluded.can_create,
    can_read   = excluded.can_read,
    can_update = excluded.can_update,
    can_delete = excluded.can_delete
RETURNING *;

-- name: GetRights :one
SELECT * FROM rights
WHERE principal_type = ? AND principal_id = ?
  AND resource_type  = ? AND resource_id  = ?;

-- name: ListPrincipalRights :many
SELECT * FROM rights
WHERE principal_type = ? AND principal_id = ?
ORDER BY resource_type, resource_id;

-- name: RevokeRights :exec
DELETE FROM rights
WHERE principal_type = ? AND principal_id = ?
  AND resource_type  = ? AND resource_id  = ?;

-- name: GetUserEffectiveRights :many
SELECT r.principal_type, r.principal_id, r.resource_type, r.resource_id,
       r.can_create, r.can_read, r.can_update, r.can_delete
FROM rights r
WHERE (
    (r.principal_type = 'user'  AND r.principal_id = ?)
 OR (r.principal_type = 'group' AND r.principal_id IN (
         SELECT group_id FROM group_members WHERE user_id = ?
     ))
);
```

- [ ] **Step 7: Replace audit.sql**

```sql
-- name: CreateAuditEvent :exec
INSERT INTO audit_log (id, principal, action, resource, detail)
VALUES (?, ?, ?, ?, ?);

-- name: ListAuditEvents :many
SELECT * FROM audit_log
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: ListAuditEventsByPrincipal :many
SELECT * FROM audit_log
WHERE principal = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: ListAuditEventsByAction :many
SELECT * FROM audit_log
WHERE action = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;
```

- [ ] **Step 8: Regenerate sqlc**

```bash
make sqlc
```

Expected: no errors. The `internal/db/` generated files are rewritten without `TenantID` fields and tenant-scoped parameters.

- [ ] **Step 9: Commit**

```bash
git add internal/db/queries/ internal/db/
git commit -m "feat(db): remove tenant_id from all SQL queries and regenerate sqlc"
```

---

## Task 4: Update auth types — context.go and rbac.go

**Files:**
- Modify: `internal/auth/context.go`
- Modify: `internal/auth/rbac.go`

- [ ] **Step 1: Rewrite `internal/auth/context.go`**

Replace the entire file:

```go
package auth

import "context"

type contextKey string

const principalKey contextKey = "principal"

// UserType distinguishes the two access tiers.
type UserType string

const (
	// UserTypeAdmin has full control of the entire deployment.
	UserTypeAdmin UserType = "admin"
	// UserTypeUser can manage documents within their rights.
	UserTypeUser UserType = "user"
)

// AuthMethod records how a principal was authenticated.
type AuthMethod string

const (
	AuthMethodBasic  AuthMethod = "basic"
	AuthMethodBearer AuthMethod = "bearer"
	AuthMethodAPIKey AuthMethod = "apikey"
)

// Principal represents an authenticated caller attached to a request context.
type Principal struct {
	ID         string
	Username   string
	UserType   UserType
	AuthMethod AuthMethod
}

// IsAdmin reports whether the principal has administrator rights.
func (p Principal) IsAdmin() bool {
	return p.UserType == UserTypeAdmin
}

// WithPrincipal returns a new context carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// PrincipalFromContext retrieves the Principal stored by WithPrincipal.
// Returns the zero value and false if no principal is present.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok
}
```

- [ ] **Step 2: Update `internal/auth/rbac.go`**

Remove `PermModeInherit`, `ResourceTenant`, and the `canInherit`/`hasRight` functions. Replace the file:

```go
package auth

// PermMode controls how access is evaluated for principals with no explicit right.
type PermMode string

const (
	// PermModeExplicit: no access unless a right is explicitly granted.
	PermModeExplicit PermMode = "explicit"
	// PermModeOpen: full access unless a right exists and does not grant the action.
	PermModeOpen PermMode = "open"
)

// Action represents a permission being checked.
type Action string

const (
	ActionCreate Action = "create"
	ActionRead   Action = "read"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

// ResourceType identifies the kind of resource being accessed.
type ResourceType string

const (
	ResourceProject  ResourceType = "project"
	ResourceBucket   ResourceType = "bucket"
	ResourceDocument ResourceType = "document"
)

// ResourceAncestor is a (type, id) pair representing a parent resource in the
// hierarchy. Pass ancestors from nearest to furthest, e.g. bucket then project.
type ResourceAncestor struct {
	Type ResourceType
	ID   string
}

// Can reports whether principal p may perform action on (resourceType, resourceID).
//
// Admin principals are always permitted regardless of rights or mode.
//
// For regular users, rights is the slice returned by Store.GetUserRights or
// Store.GetAPIKeyRights. mode controls the default stance when no right is found:
//   - PermModeExplicit: denied unless an exact or wildcard right grants the action.
//   - PermModeOpen: allowed unless a right exists for this resource and doesn't
//     grant the action.
func Can(p Principal, rights []Right, mode PermMode, action Action, resourceType ResourceType, resourceID string, ancestors ...ResourceAncestor) bool {
	if p.IsAdmin() {
		return true
	}

	switch mode {
	case PermModeOpen:
		return canOpen(rights, action, resourceType, resourceID)
	default: // PermModeExplicit
		return checkRights(rights, action, resourceType, resourceID)
	}
}

// canOpen allows by default unless a right row exists for the resource and does
// not grant the requested action.
func canOpen(rights []Right, action Action, resourceType ResourceType, resourceID string) bool {
	for _, r := range rights {
		if ResourceType(r.ResourceType) != resourceType {
			continue
		}
		if r.ResourceID != resourceID && r.ResourceID != "*" {
			continue
		}
		return rightGrantsAction(r, action)
	}
	return true
}

// checkRights returns true if any right in the slice grants action on
// (resourceType, resourceID) or (resourceType, "*").
func checkRights(rights []Right, action Action, resourceType ResourceType, resourceID string) bool {
	for _, r := range rights {
		if ResourceType(r.ResourceType) != resourceType {
			continue
		}
		if r.ResourceID != resourceID && r.ResourceID != "*" {
			continue
		}
		if rightGrantsAction(r, action) {
			return true
		}
	}
	return false
}

func rightGrantsAction(r Right, action Action) bool {
	switch action {
	case ActionCreate:
		return r.CanCreate
	case ActionRead:
		return r.CanRead
	case ActionUpdate:
		return r.CanUpdate
	case ActionDelete:
		return r.CanDelete
	}
	return false
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/auth/context.go internal/auth/rbac.go
git commit -m "feat(auth): remove superadmin role, TenantID from Principal, ResourceTenant from RBAC"
```

---

## Task 5: Update auth/token.go — remove TenantID from JWT

**Files:**
- Modify: `internal/auth/token.go`

- [ ] **Step 1: Update Claims struct and NewJWT**

Edit `internal/auth/token.go`. Remove `TenantID` from `Claims` and from `NewJWT`'s signature:

```go
// Claims are the custom JWT claims used by TinyDM.
type Claims struct {
	Username string `json:"usr"`
	UserType string `json:"typ"`
	jwt.RegisteredClaims
}

// NewJWT creates a signed HS256 JWT for the given user.
func NewJWT(secret string, expiryMinutes int, userID, username string, userType UserType) (string, error) {
	claims := Claims{
		Username: username,
		UserType: string(userType),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expiryMinutes) * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}
```

`ParseJWT` is unchanged — it returns `*Claims` which no longer has `TenantID`.

- [ ] **Step 2: Commit**

```bash
git add internal/auth/token.go
git commit -m "feat(auth): remove TenantID claim from JWT"
```

---

## Task 6: Update auth/store.go — remove all tenant-scoped raw SQL

**Files:**
- Modify: `internal/auth/store.go`

This file uses raw SQL. Every query that filters by `tenant_id` must be rewritten. Key changes:

- `User` struct: remove `TenantID` field
- `APIKey` struct: remove `TenantID` field
- `GetUserByUsername(ctx, tenantID, username)` → `GetUserByUsername(ctx, username)`
- `GetUserByEmail(ctx, tenantID, email)` → `GetUserByEmail(ctx, email)`
- `CreateUser(ctx, tenantID, ...)` → `CreateUser(ctx, ...)` (no tenantID param)
- `ListUsers(ctx, tenantID, ...)` → `ListUsers(ctx, ...)`
- `CountUsersByTenant` → delete
- `CountAPIKeysByTenant` → delete
- `CreateAPIKey(ctx, tenantID, ...)` → `CreateAPIKey(ctx, ...)` (no tenantID)
- `ListAPIKeys(ctx, tenantID, ...)` → `ListAPIKeys(ctx, ...)`
- `RevokeAPIKey(ctx, tenantID, id)` → `RevokeAPIKey(ctx, id)`
- `GetUserRights(ctx, tenantID, userID)` → `GetUserRights(ctx, userID)`
- `GetAPIKeyRights(ctx, tenantID, keyID)` → `GetAPIKeyRights(ctx, keyID)`
- `UpsertRightParams`: remove `TenantID` field
- `UpsertRight` SQL: remove `tenant_id` from INSERT
- `DeleteRight(ctx, tenantID, ...)` → `DeleteRight(ctx, ...)`
- `ListRights(ctx, tenantID, ...)` → `ListRights(ctx, ...)`
- `ListRightsByResource(ctx, tenantID, ...)` → `ListRightsByResource(ctx, ...)`
- `GetTenantPermMode` → delete
- `SetTenantPermMode` → delete
- `CreateDomainAdmin` → delete
- `EnsureAdminUser(ctx, tenantID, tenantName, username, email, pass)` → `EnsureAdminUser(ctx, username, email, pass)`
- `SetUserActive`: remove `UserTypeSuperAdmin` guard (only guard is `UserTypeAdmin`)
- `DeleteUser`: remove `UserTypeSuperAdmin` guard

- [ ] **Step 1: Update `User` and `APIKey` structs**

In `internal/auth/store.go`, change the struct definitions:

```go
type User struct {
	ID           string
	Username     string
	Email        string
	FirstName    string
	LastName     string
	PasswordHash string
	UserType     UserType
	IsActive     bool
}

type APIKey struct {
	ID        string
	UserID    sql.NullString
	Name      string
	KeyHash   string
	KeyPrefix string
	ExpiresAt sql.NullTime
	RevokedAt sql.NullTime
}
```

- [ ] **Step 2: Update `scanUser` helper**

Find the `scanUser` function (likely at the bottom of store.go) and remove the `TenantID` field from the Scan call. The SELECT queries no longer return `tenant_id`, so the scan must match:

```go
func scanUser(row interface{ Scan(...interface{}) error }) (*User, error) {
	var u User
	var isActive int
	err := row.Scan(&u.ID, &u.Username, &u.Email,
		&u.FirstName, &u.LastName,
		&u.PasswordHash, &u.UserType, &isActive)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.IsActive = isActive == 1
	return &u, nil
}
```

- [ ] **Step 3: Update `GetUserByUsername`**

```go
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, email, first_name, last_name, password_hash, user_type, is_active
		FROM users
		WHERE username = ? AND deleted_at IS NULL`,
		username,
	)
	return scanUser(row)
}
```

- [ ] **Step 4: Update `GetUserByID`**

```go
func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, email, first_name, last_name, password_hash, user_type, is_active
		FROM users
		WHERE id = ? AND deleted_at IS NULL`,
		id,
	)
	return scanUser(row)
}
```

- [ ] **Step 5: Update `CreateUser`**

```go
func (s *Store) CreateUser(ctx context.Context, username, email, firstName, lastName, passwordHash string, userType UserType) (*User, error) {
	id := uuid.New().String()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, email, first_name, last_name, password_hash, user_type)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, username, email, firstName, lastName, passwordHash, string(userType),
	)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return s.GetUserByID(ctx, id)
}
```

- [ ] **Step 6: Update `ListUsers`**

```go
func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]*User, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, email, first_name, last_name, password_hash, user_type, is_active
		FROM users
		WHERE deleted_at IS NULL
		ORDER BY username
		LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		var u User
		var isActive int
		if err := rows.Scan(&u.ID, &u.Username, &u.Email,
			&u.FirstName, &u.LastName,
			&u.PasswordHash, &u.UserType, &isActive); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		u.IsActive = isActive == 1
		users = append(users, &u)
	}
	return users, total, rows.Err()
}
```

- [ ] **Step 7: Update `SetUserActive` and `DeleteUser`** — remove `UserTypeSuperAdmin` guard

```go
func (s *Store) SetUserActive(ctx context.Context, id string, active bool) error {
	if !active {
		u, err := s.GetUserByID(ctx, id)
		if err != nil {
			return fmt.Errorf("set active: %w", err)
		}
		if u != nil && u.UserType == UserTypeAdmin {
			return fmt.Errorf("admin accounts cannot be deactivated")
		}
	}
	val := 0
	if active {
		val = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET is_active = ? WHERE id = ? AND deleted_at IS NULL`,
		val, id,
	)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	u, err := s.GetUserByID(ctx, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if u != nil && u.UserType == UserTypeAdmin {
		return fmt.Errorf("admin account cannot be deleted")
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL`,
		id,
	)
	return err
}
```

- [ ] **Step 8: Update API key methods**

Update `GetAPIKeyByHash` and `GetAPIKeyByID` — remove `tenant_id` from SELECT and Scan:

```go
func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, key_hash, key_prefix, expires_at, revoked_at
		FROM api_keys
		WHERE key_hash = ?`,
		hash,
	)
	var k APIKey
	if err := row.Scan(&k.ID, &k.UserID, &k.Name,
		&k.KeyHash, &k.KeyPrefix, &k.ExpiresAt, &k.RevokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get api key by hash: %w", err)
	}
	return &k, nil
}

func (s *Store) GetAPIKeyByID(ctx context.Context, id string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, key_hash, key_prefix, expires_at, revoked_at
		FROM api_keys
		WHERE id = ?`,
		id,
	)
	var k APIKey
	if err := row.Scan(&k.ID, &k.UserID, &k.Name,
		&k.KeyHash, &k.KeyPrefix, &k.ExpiresAt, &k.RevokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get api key by id: %w", err)
	}
	return &k, nil
}
```

Update `CreateAPIKey` (remove `tenantID` param):

```go
func (s *Store) CreateAPIKey(ctx context.Context, userID *string, name, keyHash, keyPrefix string, expiresAt *time.Time) (*APIKey, error) {
	id := uuid.New().String()
	var uid sql.NullString
	if userID != nil {
		uid = sql.NullString{String: *userID, Valid: true}
	}
	var exp sql.NullTime
	if expiresAt != nil {
		exp = sql.NullTime{Time: *expiresAt, Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_keys (id, user_id, name, key_hash, key_prefix, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, uid, name, keyHash, keyPrefix, exp,
	)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return s.GetAPIKeyByHash(ctx, keyHash)
}
```

Update `ListAPIKeys` (remove `tenantID` param):

```go
func (s *Store) ListAPIKeys(ctx context.Context, limit, offset int) ([]*APIKey, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL`,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count api keys: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, name, key_hash, key_prefix, expires_at, revoked_at
		FROM api_keys
		WHERE revoked_at IS NULL
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()
	var keys []*APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name,
			&k.KeyHash, &k.KeyPrefix, &k.ExpiresAt, &k.RevokedAt); err != nil {
			return nil, 0, fmt.Errorf("scan api key: %w", err)
		}
		keys = append(keys, &k)
	}
	return keys, total, rows.Err()
}
```

Update `RevokeAPIKey` (remove `tenantID`):

```go
func (s *Store) RevokeAPIKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND revoked_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("api key not found or already revoked")
	}
	return nil
}
```

- [ ] **Step 9: Update RBAC methods**

Update `GetUserRights` (remove `tenantID`):

```go
func (s *Store) GetUserRights(ctx context.Context, userID string) ([]Right, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.principal_type, r.principal_id, r.resource_type, r.resource_id,
		       r.can_create, r.can_read, r.can_update, r.can_delete
		FROM rights r
		WHERE (
		    (r.principal_type = 'user'  AND r.principal_id = ?)
		 OR (r.principal_type = 'group' AND r.principal_id IN (
		         SELECT group_id FROM group_members WHERE user_id = ?
		     ))
		)`,
		userID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query rights: %w", err)
	}
	defer rows.Close()
	return scanRights(rows)
}
```

Update `GetAPIKeyRights` (remove `tenantID`):

```go
func (s *Store) GetAPIKeyRights(ctx context.Context, apiKeyID string) ([]Right, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT principal_type, principal_id, resource_type, resource_id,
		       can_create, can_read, can_update, can_delete
		FROM rights
		WHERE principal_type = 'apikey' AND principal_id = ?`,
		apiKeyID,
	)
	if err != nil {
		return nil, fmt.Errorf("get api key rights: %w", err)
	}
	defer rows.Close()
	return scanRights(rows)
}
```

Update `UpsertRightParams` and `UpsertRight` (remove `TenantID`):

```go
type UpsertRightParams struct {
	PrincipalType string
	PrincipalID   string
	ResourceType  string
	ResourceID    string
	CanCreate     bool
	CanRead       bool
	CanUpdate     bool
	CanDelete     bool
}

func (s *Store) UpsertRight(ctx context.Context, p UpsertRightParams) error {
	id := uuid.New().String()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rights
		    (id, principal_type, principal_id, resource_type, resource_id,
		     can_create, can_read, can_update, can_delete)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(principal_type, principal_id, resource_type, resource_id)
		DO UPDATE SET
		    can_create = excluded.can_create,
		    can_read   = excluded.can_read,
		    can_update = excluded.can_update,
		    can_delete = excluded.can_delete`,
		id, p.PrincipalType, p.PrincipalID,
		p.ResourceType, p.ResourceID,
		boolToInt(p.CanCreate), boolToInt(p.CanRead),
		boolToInt(p.CanUpdate), boolToInt(p.CanDelete),
	)
	return err
}
```

Update `DeleteRight`, `ListRights`, `ListRightsByResource` (remove `tenantID`):

```go
func (s *Store) DeleteRight(ctx context.Context, principalType, principalID, resourceType, resourceID string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM rights
		WHERE principal_type = ? AND principal_id = ?
		  AND resource_type = ? AND resource_id = ?`,
		principalType, principalID, resourceType, resourceID,
	)
	if err != nil {
		return fmt.Errorf("delete right: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("right not found")
	}
	return nil
}

func (s *Store) ListRights(ctx context.Context, principalType, principalID string) ([]Right, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT principal_type, principal_id, resource_type, resource_id,
		       can_create, can_read, can_update, can_delete
		FROM rights
		WHERE principal_type = ? AND principal_id = ?
		ORDER BY resource_type, resource_id`,
		principalType, principalID,
	)
	if err != nil {
		return nil, fmt.Errorf("list rights: %w", err)
	}
	defer rows.Close()
	return scanRights(rows)
}

func (s *Store) ListRightsByResource(ctx context.Context, resourceType, resourceID string) ([]Right, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT principal_type, principal_id, resource_type, resource_id,
		       can_create, can_read, can_update, can_delete
		FROM rights
		WHERE resource_type = ? AND resource_id = ?
		ORDER BY principal_type, principal_id`,
		resourceType, resourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list rights by resource: %w", err)
	}
	defer rows.Close()
	return scanRights(rows)
}
```

- [ ] **Step 10: Update `EnsureAdminUser` — remove tenant creation**

```go
func (s *Store) EnsureAdminUser(ctx context.Context, username, email, password string) error {
	n, err := s.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		return nil // already bootstrapped
	}
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}
	if _, err := s.CreateUser(ctx, username, email, "Admin", "", hash, UserTypeAdmin); err != nil {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}
	return nil
}
```

- [ ] **Step 11: Delete `GetTenantPermMode`, `SetTenantPermMode`, `CreateDomainAdmin` functions**

Remove these three functions entirely from `store.go`.

- [ ] **Step 12: Delete `CountUsersByTenant` and `CountAPIKeysByTenant` functions**

Remove these two functions entirely from `store.go`.

- [ ] **Step 13: Commit**

```bash
git add internal/auth/store.go
git commit -m "feat(auth): remove tenant-scoped raw SQL from auth store"
```

---

## Task 7: Update auth/middleware.go

**Files:**
- Modify: `internal/auth/middleware.go`

- [ ] **Step 1: Remove `RequireSuperAdmin` and fix `verifyBasic` and `verifyAPIKey`**

Remove the entire `RequireSuperAdmin` function. Update `verifyBasic` to not require `X-Tenant-ID` and update `verifyJWT`/`verifyAPIKey` to not set `TenantID`:

```go
func identify(r *http.Request, secret string, store *Store) (*Principal, error) {
	authHeader := r.Header.Get("Authorization")

	if strings.HasPrefix(authHeader, "Bearer ") {
		return verifyJWT(r.Context(), secret, strings.TrimPrefix(authHeader, "Bearer "))
	}
	if strings.HasPrefix(authHeader, "Basic ") {
		return verifyBasic(r.Context(), strings.TrimPrefix(authHeader, "Basic "), store)
	}
	if key := r.Header.Get("X-API-Key"); key != "" {
		return verifyAPIKey(r.Context(), key, store)
	}
	return nil, nil
}

func verifyJWT(_ context.Context, secret, tokenStr string) (*Principal, error) {
	claims, err := ParseJWT(secret, tokenStr)
	if err != nil {
		return nil, fmt.Errorf("jwt: %w", err)
	}
	return &Principal{
		ID:         claims.Subject,
		Username:   claims.Username,
		UserType:   UserType(claims.UserType),
		AuthMethod: AuthMethodBearer,
	}, nil
}

func verifyBasic(ctx context.Context, encoded string, store *Store) (*Principal, error) {
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("basic: decode: %w", err)
	}
	parts := strings.SplitN(string(b), ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("basic: expected username:password")
	}
	username, password := parts[0], parts[1]

	user, err := store.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("basic: lookup user: %w", err)
	}
	if user == nil || !user.IsActive {
		return nil, fmt.Errorf("basic: user not found or inactive")
	}
	if err := CheckPassword(user.PasswordHash, password); err != nil {
		return nil, fmt.Errorf("basic: invalid password")
	}
	return &Principal{
		ID:         user.ID,
		Username:   user.Username,
		UserType:   user.UserType,
		AuthMethod: AuthMethodBasic,
	}, nil
}

func verifyAPIKey(ctx context.Context, key string, store *Store) (*Principal, error) {
	hash := HashAPIKey(key)

	apiKey, err := store.GetAPIKeyByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("apikey: lookup: %w", err)
	}
	if apiKey == nil {
		return nil, fmt.Errorf("apikey: not found")
	}
	if apiKey.RevokedAt.Valid {
		return nil, fmt.Errorf("apikey: revoked")
	}
	if apiKey.ExpiresAt.Valid && time.Now().After(apiKey.ExpiresAt.Time) {
		return nil, fmt.Errorf("apikey: expired")
	}

	go func() { _ = store.TouchAPIKey(context.Background(), apiKey.ID) }()

	if apiKey.UserID.Valid {
		user, err := store.GetUserByID(ctx, apiKey.UserID.String)
		if err == nil && user != nil && user.IsActive {
			return &Principal{
				ID:         user.ID,
				Username:   user.Username,
				UserType:   user.UserType,
				AuthMethod: AuthMethodAPIKey,
			}, nil
		}
	}

	// Unscoped key → treated as admin.
	return &Principal{
		ID:         apiKey.ID,
		Username:   apiKey.Name,
		UserType:   UserTypeAdmin,
		AuthMethod: AuthMethodAPIKey,
	}, nil
}
```

Also update `RequireAdmin` doc comment (no longer mentions superadmin):

```go
// RequireAdmin rejects non-admin principals with HTTP 403.
func RequireAdmin(next http.Handler) http.Handler {
```

- [ ] **Step 2: Commit**

```bash
git add internal/auth/middleware.go
git commit -m "feat(auth): remove RequireSuperAdmin and X-Tenant-ID from middleware"
```

---

## Task 8: Update config/config.go

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Remove bootstrap tenant fields and add PermMode**

In the `Config` struct, remove `BootstrapTenantID` and `BootstrapTenantName`, and add `PermMode`:

```go
// PermMode controls how RBAC access is evaluated for regular users system-wide.
// Values: "explicit" (deny by default) or "open" (allow by default). Default: "open".
PermMode string // TINYDM_PERM_MODE (default: "open")

// Bootstrap — used only on the very first run when the DB has no users.
BootstrapAdminUser  string // TINYDM_BOOTSTRAP_ADMIN_USER  (default: "admin")
BootstrapAdminEmail string // TINYDM_BOOTSTRAP_ADMIN_EMAIL (default: "")
BootstrapAdminPass  string // TINYDM_BOOTSTRAP_ADMIN_PASS  — required for bootstrap
```

In `Load()`, remove the two removed env reads and add:

```go
PermMode:           getEnv("TINYDM_PERM_MODE", "open"),
BootstrapAdminUser: getEnv("TINYDM_BOOTSTRAP_ADMIN_USER", "admin"),
BootstrapAdminEmail: getEnv("TINYDM_BOOTSTRAP_ADMIN_EMAIL", ""),
BootstrapAdminPass:  getEnv("TINYDM_BOOTSTRAP_ADMIN_PASS", ""),
```

Also add validation:

```go
if cfg.PermMode != "explicit" && cfg.PermMode != "open" {
    return nil, fmt.Errorf("TINYDM_PERM_MODE must be explicit or open, got %q", cfg.PermMode)
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): remove bootstrap tenant vars; add TINYDM_PERM_MODE"
```

---

## Task 9: Update internal/api/auth.go

**Files:**
- Modify: `internal/api/auth.go`

- [ ] **Step 1: Remove tenant_id from login request/response and Me response**

```go
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token    string `json:"token"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	UserType string `json:"user_type"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	user, err := h.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user == nil || !user.IsActive {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err := auth.CheckPassword(user.PasswordHash, req.Password); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := auth.NewJWT(
		h.cfg.JWTSecret,
		h.cfg.JWTExpiryMinutes,
		user.ID,
		user.Username,
		user.UserType,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		Token:    token,
		UserID:   user.ID,
		Username: user.Username,
		UserType: string(user.UserType),
	})
}

type meResponse struct {
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	UserType   string `json:"user_type"`
	AuthMethod string `json:"auth_method"`
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		UserID:     p.ID,
		Username:   p.Username,
		UserType:   string(p.UserType),
		AuthMethod: string(p.AuthMethod),
	})
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/api/auth.go
git commit -m "feat(api): remove tenant_id from login and me endpoints"
```

---

## Task 10: Update internal/api/context.go

**Files:**
- Modify: `internal/api/context.go`

- [ ] **Step 1: Remove TenantCtx, simplify RightsCtx and CanMiddleware, fix ProjectCtx**

Remove the `tenantKey`, `permModeKey` constants, the `TenantCtx()` function, and `tenantFromCtx()`.

Update `RightsCtx` — no longer needs a tenant:

```go
func RightsCtx(authStore *auth.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.PrincipalFromContext(r.Context())
			if !ok || p.IsAdmin() {
				next.ServeHTTP(w, r)
				return
			}
			var rights []auth.Right
			if p.AuthMethod == auth.AuthMethodAPIKey {
				rights, _ = authStore.GetAPIKeyRights(r.Context(), p.ID)
			} else {
				rights, _ = authStore.GetUserRights(r.Context(), p.ID)
			}
			ctx := contextWith(r.Context(), rightsKey, rights)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
```

Update `CanMiddleware` — remove `authStore` param, take `mode` directly:

```go
func CanMiddleware(
	mode auth.PermMode,
	action auth.Action,
	resourceType auth.ResourceType,
	getResourceID func(*http.Request) string,
	getAncestors func(*http.Request) []auth.ResourceAncestor,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.PrincipalFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if p.IsAdmin() {
				next.ServeHTTP(w, r)
				return
			}
			rights, _ := r.Context().Value(rightsKey).([]auth.Right)
			resourceID := "*"
			if getResourceID != nil {
				resourceID = getResourceID(r)
			}
			var ancestors []auth.ResourceAncestor
			if getAncestors != nil {
				ancestors = getAncestors(r)
			}
			if !auth.Can(p, rights, mode, action, resourceType, resourceID, ancestors...) {
				writeError(w, http.StatusForbidden, "permission denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

Update `ProjectCtx` — remove tenant ownership check:

```go
func ProjectCtx(store *repo.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "projectID")
			project, err := store.GetProject(r.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if project == nil {
				writeError(w, http.StatusNotFound, "project not found")
				return
			}
			next.ServeHTTP(w, r.WithContext(contextWith(r.Context(), projectKey, project)))
		})
	}
}
```

Remove the `tenantKey` and `permModeKey` constants from the `const` block. Remove the `tenantFromCtx` function.

- [ ] **Step 2: Commit**

```bash
git add internal/api/context.go
git commit -m "feat(api): remove TenantCtx; simplify RightsCtx and CanMiddleware"
```

---

## Task 11: Update internal/api/security.go and internal/api/users.go

**Files:**
- Modify: `internal/api/security.go`
- Modify: `internal/api/users.go`

- [ ] **Step 1: Remove `RequireSameTenant` from security.go**

Delete the entire `RequireSameTenant` function and its associated comment block. Keep `SecurityHeaders` and `sanitizeFilename` unchanged.

- [ ] **Step 2: Update users.go — remove tenantFromCtx usage**

In `ListUsers`, replace `tenant := tenantFromCtx(r)` with no tenant, and call `h.store.ListUsers(r.Context(), page.Limit, page.Offset)`.

In the `safeUser` struct, remove the `TenantID` field.

Scan through the entire `users.go` file for any remaining references to `tenant`, `TenantID`, or `tenantFromCtx` and remove them. Any calls that passed `tenant.ID` to store methods should now pass nothing or use the simplified signatures.

- [ ] **Step 3: Commit**

```bash
git add internal/api/security.go internal/api/users.go
git commit -m "feat(api): remove RequireSameTenant and tenant references from user handler"
```

---

## Task 12: Delete api/tenants.go and rewrite api/routes.go

**Files:**
- Delete: `internal/api/tenants.go`
- Modify: `internal/api/routes.go`

- [ ] **Step 1: Delete tenants.go**

```bash
rm internal/api/tenants.go
```

- [ ] **Step 2: Rewrite routes.go**

Replace the entire file. The key changes: remove the `tenantHandler`, remove the `r.Route("/tenants/{tenantID}", ...)` block, mount all routes directly under `/api/v1/`, and update all `CanMiddleware` calls to pass `auth.PermMode(cfg.PermMode)` instead of `authStore`:

```go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"tinydm/internal/audit"
	"tinydm/internal/auth"
	"tinydm/internal/cluster"
	"tinydm/internal/config"
	"tinydm/internal/repo"
	"tinydm/internal/storage"
)

// RegisterRoutes mounts all API v1 routes onto r.
// All routes under /api/v1 (except /auth/login) require authentication.
//
// Role matrix:
//
//	admin — full control (projects, buckets, users, audit)
//	user  — manage documents, tags, and properties within their rights
func RegisterRoutes(r chi.Router, cfg *config.Config, repoStore *repo.Store, authStore *auth.Store, store storage.Store, auditStore *audit.Store, locker cluster.Locker) {
	mode := auth.PermMode(cfg.PermMode)

	authHandler := NewAuthHandler(cfg, authStore)
	projectHandler := NewProjectHandler(repoStore, authStore)
	bucketHandler := NewBucketHandler(repoStore, authStore)
	docHandler := NewDocumentHandler(repoStore, authStore, store, locker)
	tagHandler := NewTagHandler(repoStore, locker)
	propHandler := NewPropertyHandler(repoStore, locker)
	auditHandler := NewAuditHandler(auditStore)
	userHandler := NewUserHandler(authStore)

	r.Route("/api/v1", func(r chi.Router) {

		// Public
		r.Post("/auth/login", authHandler.Login)

		// Authenticated
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Use(audit.Middleware(auditStore))

			// Auth
			r.Get("/auth/me", authHandler.Me)

			// Audit log — admin only
			r.With(auth.RequireAdmin).Get("/audit", auditHandler.List)

			// Users — admin only
			r.With(auth.RequireAdmin).Get("/users", userHandler.ListUsers)
			r.With(auth.RequireAdmin).Post("/users", userHandler.CreateUser)
			r.With(auth.RequireAdmin).Patch("/users/{userID}/password", userHandler.ChangePassword)
			r.With(auth.RequireAdmin).Post("/users/{userID}/activate", userHandler.ActivateUser)
			r.With(auth.RequireAdmin).Post("/users/{userID}/deactivate", userHandler.DeactivateUser)
			r.With(auth.RequireAdmin).Delete("/users/{userID}", userHandler.DeleteUser)

			// API keys — admin only
			r.With(auth.RequireAdmin).Get("/apikeys", userHandler.ListAPIKeys)
			r.With(auth.RequireAdmin).Post("/apikeys", userHandler.CreateAPIKey)
			r.With(auth.RequireAdmin).Post("/apikeys/{keyID}/revoke", userHandler.RevokeAPIKey)

			// Rights
			r.With(auth.RequireAdmin).Get("/rights", userHandler.ListRights)
			r.With(auth.RequireAdmin).Post("/rights", userHandler.UpsertRight)
			r.With(auth.RequireAdmin).Delete("/rights", userHandler.DeleteRight)

			r.Use(RightsCtx(authStore))

			// Projects
			r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceProject,
				nil, nil)).Get("/projects", projectHandler.List)
			r.With(CanMiddleware(mode, auth.ActionCreate, auth.ResourceProject,
				nil, nil)).Post("/projects", projectHandler.Create)

			r.Route("/projects/{projectID}", func(r chi.Router) {
				r.Use(ProjectCtx(repoStore))

				r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceProject,
					func(r *http.Request) string { return chi.URLParam(r, "projectID") },
					nil)).Get("/", projectHandler.Get)
				r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceProject,
					func(r *http.Request) string { return chi.URLParam(r, "projectID") },
					nil)).Put("/", projectHandler.Update)
				r.With(CanMiddleware(mode, auth.ActionDelete, auth.ResourceProject,
					func(r *http.Request) string { return chi.URLParam(r, "projectID") },
					nil)).Delete("/", projectHandler.Delete)

				// Buckets
				r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceBucket,
					nil,
					func(r *http.Request) []auth.ResourceAncestor {
						return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
					})).Get("/buckets", bucketHandler.List)
				r.With(CanMiddleware(mode, auth.ActionCreate, auth.ResourceBucket,
					nil,
					func(r *http.Request) []auth.ResourceAncestor {
						return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
					})).Post("/buckets", bucketHandler.Create)

				r.Route("/buckets/{bucketID}", func(r chi.Router) {
					r.Use(BucketCtx(repoStore))

					r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceBucket,
						func(r *http.Request) string { return chi.URLParam(r, "bucketID") },
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
						})).Get("/", bucketHandler.Get)
					r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceBucket,
						func(r *http.Request) string { return chi.URLParam(r, "bucketID") },
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
						})).Put("/", bucketHandler.Update)
					r.With(CanMiddleware(mode, auth.ActionDelete, auth.ResourceBucket,
						func(r *http.Request) string { return chi.URLParam(r, "bucketID") },
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
						})).Delete("/", bucketHandler.Delete)

					// Documents
					r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceDocument,
						nil,
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{
								{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
								{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
							}
						})).Get("/documents", docHandler.List)
					r.With(CanMiddleware(mode, auth.ActionCreate, auth.ResourceDocument,
						nil,
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{
								{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
								{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
							}
						})).Post("/documents", docHandler.Upload)

					r.Route("/documents/{documentID}", func(r chi.Router) {
						r.Use(DocumentCtx(repoStore))

						r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/", docHandler.Get)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Put("/", docHandler.Update)
						r.With(CanMiddleware(mode, auth.ActionDelete, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Delete("/", docHandler.Delete)
						r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/content", docHandler.Download)

						// Versions
						r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/versions", docHandler.ListVersions)
						r.Route("/versions/{versionID}", func(r chi.Router) {
							r.Use(VersionCtx(repoStore))
							r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Post("/restore", docHandler.RestoreVersion)
						})

						// Tags and properties
						r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/tags", tagHandler.List)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Put("/tags", tagHandler.Replace)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Post("/tags/{tag}", tagHandler.Add)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Delete("/tags/{tag}", tagHandler.Remove)

						r.With(CanMiddleware(mode, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/properties", propHandler.List)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Put("/properties", propHandler.Replace)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Put("/properties/{key}", propHandler.Set)
						r.With(CanMiddleware(mode, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Delete("/properties/{key}", propHandler.Delete)
					})
				})
			})
		})
	})

	// Health — no auth
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// API docs
	r.Get("/api/docs", serveSwaggerUI)
	r.Get("/api/docs/openapi.yaml", serveOpenAPISpec)
}
```

Note: The `userHandler.CreateUser`, `ActivateUser`, `DeactivateUser`, `DeleteUser`, `ListRights`, `UpsertRight`, `DeleteRight` method names may differ from existing names. Check the existing `users.go` handler method names and use the correct ones. If `CreateUser` is not a handler method yet, add it following the same pattern as `ListUsers`.

- [ ] **Step 3: Commit**

```bash
git add internal/api/tenants.go internal/api/routes.go
git commit -m "feat(api): delete tenant handler; flatten all routes to /api/v1/"
```

---

## Task 13: Update cmd/tinydm/main.go

**Files:**
- Modify: `cmd/tinydm/main.go`

- [ ] **Step 1: Update the bootstrap call**

Find the bootstrap block and replace it:

```go
if cfg.BootstrapAdminPass != "" {
    if err := authStore.EnsureAdminUser(
        context.Background(),
        cfg.BootstrapAdminUser,
        cfg.BootstrapAdminEmail,
        cfg.BootstrapAdminPass,
    ); err != nil {
        slog.Error("bootstrap failed", "error", err)
        os.Exit(1)
    }
    slog.Info("bootstrap complete (no-op if users already exist)",
        "user", cfg.BootstrapAdminUser,
    )
}
```

- [ ] **Step 2: Commit**

```bash
git add cmd/tinydm/main.go
git commit -m "feat(main): update bootstrap to not create tenant"
```

---

## Task 14: Update web/web.go — flatten routes

**Files:**
- Modify: `internal/web/web.go`

- [ ] **Step 1: Update the page list in parseTemplates**

Remove `"tenants"` from the pages slice:

```go
pages := []string{
    "dashboard", "projects", "buckets",
    "documents", "docdetail", "users", "apikeys", "audit",
}
```

- [ ] **Step 2: Rewrite `RegisterRoutes` — remove tenant path segments**

Remove the `/admin/tenants` routes and flatten all routes:

```go
// Tenants — REMOVED

// Projects (was /admin/tenants/{tenantID}/projects)
r.Get("/admin/projects", h.projects)
r.Post("/admin/projects", h.createProject)
r.Delete("/admin/projects/{projectID}", h.deleteProject)

// Buckets
r.Get("/admin/projects/{projectID}/buckets", h.buckets)
r.Post("/admin/projects/{projectID}/buckets", h.createBucket)
r.Get("/admin/projects/{projectID}/buckets/{bucketID}/edit", h.editBucketForm)
r.Get("/admin/projects/{projectID}/buckets/{bucketID}/row", h.bucketRowPartial)
r.Put("/admin/projects/{projectID}/buckets/{bucketID}", h.updateBucket)
r.Delete("/admin/projects/{projectID}/buckets/{bucketID}", h.deleteBucket)

// Documents list
r.Get("/admin/projects/{projectID}/buckets/{bucketID}/documents", h.documents)
r.Post("/admin/projects/{projectID}/buckets/{bucketID}/documents", h.uploadDocument)
r.Get("/admin/projects/{projectID}/buckets/{bucketID}/documents/rows", h.documentRows)

// Users (was /admin/tenants/{tenantID}/users)
r.Get("/admin/users", h.users)
r.Post("/admin/users", h.createUser)
// ... user row/password routes stay at /admin/users/{userID}/...

// API keys (was /admin/tenants/{tenantID}/apikeys)
r.Get("/admin/apikeys", h.apiKeys)
r.Post("/admin/apikeys", h.createAPIKey)
r.Post("/admin/apikeys/{keyID}/revoke", h.revokeAPIKey)

// Rights management (remove tenantID from paths)
r.Get("/admin/users/{userID}/rights", h.userRightsPanel)
r.Post("/admin/users/{userID}/rights", h.addUserRight)
r.Delete("/admin/users/{userID}/rights", h.removeUserRight)
r.Get("/admin/apikeys/{keyID}/rights", h.apiKeyRightsPanel)
r.Post("/admin/apikeys/{keyID}/rights", h.addAPIKeyRight)
r.Delete("/admin/apikeys/{keyID}/rights", h.removeAPIKeyRight)
r.Get("/admin/projects/{projectID}/rights", h.projectRightsPanel)
r.Post("/admin/projects/{projectID}/rights", h.addProjectRight)
r.Delete("/admin/projects/{projectID}/rights", h.removeProjectRight)
r.Get("/admin/projects/{projectID}/buckets/{bucketID}/rights", h.bucketRightsPanel)
r.Post("/admin/projects/{projectID}/buckets/{bucketID}/rights", h.addBucketRight)
r.Delete("/admin/projects/{projectID}/buckets/{bucketID}/rights", h.removeBucketRight)
r.Get("/admin/documents/{documentID}/rights", h.documentRightsPanel)
r.Post("/admin/documents/{documentID}/rights", h.addDocumentRight)
r.Delete("/admin/documents/{documentID}/rights", h.removeDocumentRight)

// Settings — perm_mode now at a global route (remove /admin/tenants/{tenantID}/settings/...)
r.Post("/admin/settings/permmode", h.setPermMode)

// Audit (was /admin/tenants/{tenantID}/audit)
r.Get("/admin/audit", h.auditLog)
r.Get("/admin/audit/events", h.auditEvents)
```

Note: Use the actual handler method names from `handlers.go`. The names above may differ slightly from the current code (e.g., `h.tenantUsers` → `h.users`, `h.tenantAPIKeys` → `h.apiKeys`).

- [ ] **Step 3: Commit**

```bash
git add internal/web/web.go
git commit -m "feat(web): flatten admin routes; remove tenant path segments"
```

---

## Task 15: Update web/handlers.go

**Files:**
- Modify: `internal/web/handlers.go`

- [ ] **Step 1: Remove `defaultTenantName`, `tenants`, `createTenant`, `deleteTenant` handlers**

Delete these four functions entirely.

- [ ] **Step 2: Simplify `loginData` struct and login handlers**

```go
type loginData struct {
	Error    string
	Username string
}

func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "login", loginData{})
}

func (h *Handler) loginSubmit(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		h.render(w, "login", loginData{Error: "Username and password are required.", Username: username})
		return
	}

	user, err := h.auth.GetUserByUsername(r.Context(), username)
	if err != nil || user == nil || !user.IsActive {
		h.render(w, "login", loginData{Error: "Invalid credentials.", Username: username})
		return
	}
	if err := auth.CheckPassword(user.PasswordHash, password); err != nil {
		h.render(w, "login", loginData{Error: "Invalid credentials.", Username: username})
		return
	}

	token, err := auth.NewJWT(h.cfg.JWTSecret, h.cfg.JWTExpiryMinutes, user.ID, user.Username, user.UserType)
	if err != nil {
		h.render(w, "login", loginData{Error: "Could not create session."})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.cfg.SecureCookies,
		MaxAge:   h.cfg.JWTExpiryMinutes * 60,
	})
	http.Redirect(w, r, "/admin/", http.StatusFound)
}
```

- [ ] **Step 3: Update dashboard — remove Tenants stat, remove TenantID scoping**

```go
type dashboardStats struct {
	Users     int
	Projects  int
	Buckets   int
	Documents int
}

type dashboardData struct {
	basePage
	Stats       dashboardStats
	RecentAudit []*audit.Event
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	bp := h.base(r, "dashboard")
	var stats dashboardStats
	stats.Users, _ = h.auth.CountUsers(r.Context())
	stats.Projects, _ = h.repo.CountProjects(r.Context())
	stats.Buckets, _ = h.repo.CountBuckets(r.Context())
	stats.Documents, _ = h.repo.CountDocuments(r.Context())

	recent, _, _ := h.audit.List(r.Context(), audit.Filter{Limit: 10})

	h.render(w, "dashboard", dashboardData{
		basePage:    bp,
		Stats:       stats,
		RecentAudit: recent,
	})
}
```

Note: `h.repo.CountProjects`, `CountBuckets`, `CountDocuments` previously took a `tenantID` argument. After the sqlc regeneration these will have no tenant param. The `audit.Filter` previously had a `TenantID` field — remove it from all call sites in `handlers.go`.

- [ ] **Step 4: Update `requireSession` middleware — remove TenantID from Principal**

```go
ctx := auth.WithPrincipal(r.Context(), auth.Principal{
    ID:       user.ID,
    Username: user.Username,
    UserType: user.UserType,
})
```

- [ ] **Step 5: Update all remaining handlers that reference tenantID**

Scan `handlers.go` for all remaining uses of:
- `tenant.ID` / `tenantID` — remove tenant argument from store calls
- `p.TenantID` — remove (Principal no longer has this field)
- `h.repo.ListTenants` / `h.repo.GetTenant` / `h.repo.GetTenantByName` / `h.repo.CreateTenant` — remove all these call sites
- Any handler that fetches the tenant from the URL param `{tenantID}` — remove the fetch, use the path param directly as a resource lookup without tenant scoping

For project/bucket handlers: remove calls to `h.repo.GetTenant` to validate ownership — ownership is no longer tenant-scoped. Simplify to just look up the project/bucket by ID.

For user/apikey/audit handlers: remove `tenantID` from all store calls (e.g., `h.auth.ListUsers(ctx, tenantID, ...)` → `h.auth.ListUsers(ctx, ...)`).

- [ ] **Step 6: Update `setPermMode` handler**

The `setPermMode` handler previously called `h.auth.SetTenantPermMode(ctx, tenantID, mode)`. That method is deleted. This route should now be removed or reimplemented as a config-only operation (not runtime-settable). The simplest fix: remove the handler and its route entirely. The perm mode is set via `TINYDM_PERM_MODE` env var.

- [ ] **Step 7: Commit**

```bash
git add internal/web/handlers.go
git commit -m "feat(web): remove tenant handlers; simplify login and dashboard"
```

---

## Task 16: Update web templates

**Files:**
- Delete: `internal/web/templates/tenants.html`
- Modify: `internal/web/templates/login.html`
- Modify: `internal/web/templates/base.html`
- Modify: `internal/web/templates/dashboard.html`
- Modify: `internal/web/templates/projects.html` (and others with breadcrumbs/links)

- [ ] **Step 1: Delete tenants.html**

```bash
rm internal/web/templates/tenants.html
```

- [ ] **Step 2: Replace login.html — remove tenant field**

```html
{{define "login"}}<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Sign in — TinyDM</title>
  <link rel="stylesheet" href="/admin/static/style.css">
</head>
<body>
<div class="login-page">
  <div class="login-card">
    <div class="login-logo">Tiny<span>DM</span></div>
    <h1>Sign in</h1>
    <p class="subtitle">Document Management Admin</p>

    {{if .Error}}<div class="flash flash-err">{{.Error}}</div>{{end}}

    <form method="POST" action="/admin/login">
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

- [ ] **Step 3: Update base.html — remove Tenants nav link and TenantID display**

Remove the `<li><a href="/admin/tenants" ...>Tenants</a></li>` nav item.

Change the topbar to show the username instead of TenantID:

```html
<span class="topbar-user">{{.Principal.Username}}</span>
```

Add a Projects nav link:

```html
<li><a href="/admin/projects" {{if eq .Page "projects"}}class="active"{{end}}>
  <span class="icon">⬡</span> Projects
</a></li>
```

- [ ] **Step 4: Update dashboard.html — remove Tenants stat card**

Remove the stat card that displayed `.Stats.Tenants`. Update any stats references to the new struct (no `Tenants` field).

- [ ] **Step 5: Update projects.html and other templates — fix breadcrumbs and links**

Find all `href` values that include `/admin/tenants/{tenantID}/...` and update them to `/admin/projects/...` etc. Also update breadcrumb nav items that show the tenant level.

For example, the projects page breadcrumb probably shows: `Dashboard > Tenant > Projects`. Remove the Tenant level: `Dashboard > Projects`.

- [ ] **Step 6: Commit**

```bash
git add internal/web/templates/
git commit -m "feat(web): remove tenants template; update login, nav, breadcrumbs"
```

---

## Task 17: Fix and update tests

**Files:**
- Delete: `internal/api/tenants_test.go`
- Modify: `internal/api/server_test.go`
- Modify: `internal/api/auth_test.go`
- Modify: `internal/api/security_test.go`
- Modify: other test files as needed

- [ ] **Step 1: Delete tenants_test.go**

```bash
rm internal/api/tenants_test.go
```

- [ ] **Step 2: Update server_test.go — remove tenant from test helpers**

Replace `seedAdminUser` and `seedSuperadminUser` — there is no longer a tenant to create first:

```go
// seedAdminUser creates an admin user in the DB directly.
func (ts *testServer) seedAdminUser(t *testing.T, username, password string) *auth.User {
	t.Helper()
	return ts.seedUserWithType(t, username, password, auth.UserTypeAdmin)
}

// seedUserWithType is the shared implementation.
func (ts *testServer) seedUserWithType(t *testing.T, username, password string, userType auth.UserType) *auth.User {
	t.Helper()
	ctx := context.Background()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user, err := ts.authStore.CreateUser(ctx, username, username+"@test.local", "Test", "User", hash, userType)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return user
}
```

Update `login` helper — remove `tenantID` param:

```go
func (ts *testServer) login(t *testing.T, username, password string) string {
	t.Helper()
	var result map[string]any
	resp := ts.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": username,
		"password": password,
	}, nil, &result)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: got %d, want 200", resp.StatusCode)
	}
	token, ok := result["token"].(string)
	if !ok || token == "" {
		t.Fatalf("login: no token in response: %v", result)
	}
	return token
}
```

Update `uploadFile` helper — use new path, remove tenantID param:

```go
func (ts *testServer) uploadFile(t *testing.T, token, projectID, bucketID, filename string, content []byte) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	mw.Close()

	path := fmt.Sprintf("/api/v1/projects/%s/buckets/%s/documents", projectID, bucketID)
	resp := ts.do(t, http.MethodPost, path, &buf, map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  mw.FormDataContentType(),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: got %d: %s", resp.StatusCode, body)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	return doc
}
```

Also add a `seedProject` helper since tests now need to create projects directly (no tenant):

```go
func (ts *testServer) seedProject(t *testing.T, name string) *repo.Project {
	t.Helper()
	proj, err := ts.repoStore.CreateProject(context.Background(), name, "test project")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return proj
}
```

- [ ] **Step 3: Update auth_test.go**

Find all calls to `ts.login(t, tenantID, username, password)` and change to `ts.login(t, username, password)`.

Find all calls to `ts.seedAdminUser(t, tenantName, username, password)` that capture a `*repo.Tenant` return value — update to `ts.seedAdminUser(t, username, password)` (no tenant returned).

Find any test that asserts `"tenant_id"` in login response JSON — remove those assertions.

- [ ] **Step 4: Update security_test.go**

Remove all tests that test cross-tenant blocking (`TestPermissions_CrossTenantBlocked`, etc.). Keep RBAC grant/deny tests but update them to use the new helper signatures (no tenantID). For example:

```go
// Before
tenant, user := ts.seedAdminUser(t, "tenant1", "admin", "pass")
token := ts.login(t, tenant.ID, "admin", "pass")

// After
user := ts.seedAdminUser(t, "admin", "pass")
token := ts.login(t, "admin", "pass")
_ = user
```

- [ ] **Step 5: Update all remaining test files**

Scan through `documents_test.go`, `users_test.go`, `auth_test.go` for:
- Any `tenant` variable usage — remove tenant creation, keep the rest
- URL paths like `/api/v1/tenants/{id}/projects/...` — update to `/api/v1/projects/...`
- Login calls with 3 string args — update to 2 string args
- `seedAdminUser` calls with 3 string args — update to 2 string args

- [ ] **Step 6: Commit**

```bash
git add internal/api/
git commit -m "test: remove tenant isolation tests; update helpers for flat hierarchy"
```

---

## Task 18: Fix remaining compilation errors

- [ ] **Step 1: Attempt to build**

```bash
make build 2>&1 | head -80
```

Expected: compilation errors listing files and line numbers.

- [ ] **Step 2: Fix each compilation error**

Work through the error list systematically. Common categories:
- `user.TenantID undefined` — remove the field reference
- `apiKey.TenantID undefined` — remove the field reference  
- `auth.NewJWT(... tenantID ...)` — remove the tenantID argument
- `auth.UserTypeSuperAdmin undefined` — replace with `auth.UserTypeAdmin` or remove
- `auth.IsSuperAdmin()` undefined — replace with `p.IsAdmin()` or remove
- `auth.RequireSuperAdmin` undefined — remove from route chain
- `auth.ResourceTenant` undefined — remove
- `auth.PermModeInherit` undefined — remove
- `tenantFromCtx(r)` undefined — remove
- `TenantCtx(...)` undefined — remove
- `RequireSameTenant` undefined — remove
- `store.GetUserByUsername(ctx, tenantID, username)` — remove tenantID arg
- `store.CreateUser(ctx, tenantID, ...)` — remove tenantID arg
- `store.ListUsers(ctx, tenantID, ...)` — remove tenantID arg
- `store.GetTenantPermMode(...)` — remove (method deleted)
- `store.SetTenantPermMode(...)` — remove (method deleted)
- `store.CreateDomainAdmin(...)` — remove (method deleted)
- `store.CountUsersByTenant(...)` — remove (method deleted)
- `store.CountAPIKeysByTenant(...)` — remove (method deleted)
- `CanMiddleware(authStore, ...)` — update to `CanMiddleware(mode, ...)`
- `repo.ListProjects(ctx, tenantID, ...)` — remove tenantID (check actual repo method signature after sqlc regen)
- `repo.CreateProject(ctx, tenantID, ...)` — remove tenantID
- `audit.Filter{TenantID: ...}` — remove TenantID from Filter struct (in `internal/audit/`)
- `audit.Middleware` records `tenant_id` in events — update `CreateAuditEvent` call in `internal/audit/middleware.go` to match the new signature (no tenant_id parameter)

- [ ] **Step 3: Build again to verify**

```bash
make build
```

Expected: `go build` exits 0 with no errors.

- [ ] **Step 4: Commit**

```bash
git add -p  # review all changes
git commit -m "fix: resolve all compilation errors from tenant removal"
```

---

## Task 19: Run tests

- [ ] **Step 1: Run the full test suite**

```bash
make test 2>&1 | tail -40
```

Expected: `ok` for all packages. If tests fail, proceed to Step 2.

- [ ] **Step 2: Fix failing tests**

For each failing test, the failure message will show which assertion failed. Common fixes:
- URL path assertions expecting `/api/v1/tenants/{id}/...` — update to `/api/v1/...`
- Login JSON body assertions expecting `tenant_id` field — remove
- Test setup that creates a tenant before seeding users — remove tenant creation

Re-run `make test` after each round of fixes.

- [ ] **Step 3: Verify all tests pass**

```bash
make test
```

Expected output (all packages should show `ok`):
```
ok  	tinydm/internal/auth	0.XXXs
ok  	tinydm/internal/api	0.XXXs
ok  	tinydm/internal/storage	0.XXXs
ok  	tinydm/internal/meta	0.XXXs
```

- [ ] **Step 4: Final commit**

```bash
git add -p
git commit -m "test: fix all tests after tenant removal"
```

---

## Task 20: Update PLAN.md and docs

- [ ] **Step 1: Mark tenant removal as done in PLAN.md**

Add a new Phase 11 entry in `PLAN.md`:

```markdown
## Phase 11 — Remove Multi-Tenancy

| # | Task | Status | Notes |
|---|------|--------|-------|
| 11.1 | Remove tenants table and tenant_id from all tables | ✅ | Migration 007 (SQLite) / 006 (PG) |
| 11.2 | Flatten API routes to /api/v1/projects/... | ✅ | |
| 11.3 | Simplify auth to admin + user roles | ✅ | Removed superadmin |
| 11.4 | Remove tenant from JWT, Basic auth, login form | ✅ | |
| 11.5 | Remove tenant management from admin UI | ✅ | |
```

- [ ] **Step 2: Run a final build and test**

```bash
make build && make test
```

Expected: both succeed with no errors.

- [ ] **Step 3: Final commit**

```bash
git add PLAN.md
git commit -m "docs: record phase 11 tenant removal in plan"
```
