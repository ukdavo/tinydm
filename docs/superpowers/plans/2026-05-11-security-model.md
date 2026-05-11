# Security Model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the existing RBAC rights table into the API and admin UI, adding per-tenant permission modes, document-level rights, API key rights, and an admin UI for managing rights on users and API keys.

**Architecture:** Extend the `rights` table to support `apikey` principals and `document` resources; add `perm_mode` to tenants. Update `Can()` in `rbac.go` to accept a `PermMode` and ancestor chain for inheritance. Add `RightsCtx` middleware to load rights per request, replace `RequireAdmin` with `CanMiddleware` on project/bucket/document routes, and add web UI rights panels on user and API key pages.

**Tech Stack:** Go, Chi router, SQLite (goose migrations), HTMX templates, `auth.Store` raw SQL pattern.

---

## File Map

| File | Change |
|---|---|
| `internal/db/migrations/006_permission_system.sql` | New migration: widens rights CHECK constraints, adds `perm_mode` to tenants |
| `internal/auth/rbac.go` | Add `PermMode`, `ResourceAncestor`, update `Can()` signature and logic |
| `internal/auth/rbac_test.go` | Extend tests for `PermMode`, ancestor fallback, open mode |
| `internal/auth/store.go` | Add `GetAPIKeyRights`, `UpsertRight`, `DeleteRight`, `ListRights`, `GetTenantPermMode`, `SetTenantPermMode` |
| `internal/auth/store_test.go` | Tests for the six new store methods |
| `internal/api/context.go` | Extend `TenantCtx` to load `perm_mode`; add `RightsCtx` middleware; add `CanMiddleware` factory; add context keys/accessors for rights and mode |
| `internal/api/routes.go` | Replace `RequireAdmin` with `CanMiddleware` on project/bucket/document routes; add `RightsCtx` to authenticated group |
| `internal/api/security_test.go` | New file: integration tests for permission enforcement (user denied/allowed, inherit mode, admin bypass) |
| `internal/web/handlers.go` | Add `userRights`, `addUserRight`, `removeUserRight`, `apiKeyRights`, `addAPIKeyRight`, `removeAPIKeyRight`, `tenantSettings`, `setPermMode` handlers |
| `internal/web/web.go` | Register new web routes for rights panels and tenant settings |
| `internal/web/templates/users.html` | Add rights panel card (table + add-right form) below users list |
| `internal/web/templates/apikeys.html` | Add rights panel card below API keys list |
| `internal/web/templates/tenants.html` | Add "Access policy" card with perm_mode selector |

---

## Task 1: DB migration — extend rights table and add perm_mode to tenants

**Files:**
- Create: `internal/db/migrations/006_permission_system.sql`

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up
-- 006_permission_system.sql
--
-- 1. Adds perm_mode to tenants (explicit | open | inherit).
-- 2. Widens rights.principal_type to include 'apikey'.
-- 3. Widens rights.resource_type to include 'document'.
--
-- SQLite does not support ALTER TABLE ... DROP/MODIFY CONSTRAINT, so the
-- rights table is recreated using the standard copy-rename pattern.

-- 1. Extend tenants with perm_mode.
ALTER TABLE tenants ADD COLUMN perm_mode TEXT NOT NULL DEFAULT 'explicit'
    CHECK(perm_mode IN ('explicit','open','inherit'));

-- 2. Recreate rights with widened CHECK constraints.
CREATE TABLE rights_v2 (
    id             TEXT    NOT NULL PRIMARY KEY,
    tenant_id      TEXT    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    principal_type TEXT    NOT NULL CHECK(principal_type IN ('user','group','apikey')),
    principal_id   TEXT    NOT NULL,
    resource_type  TEXT    NOT NULL CHECK(resource_type IN ('tenant','project','bucket','document')),
    resource_id    TEXT    NOT NULL DEFAULT '*',
    can_create     INTEGER NOT NULL DEFAULT 0,
    can_read       INTEGER NOT NULL DEFAULT 0,
    can_update     INTEGER NOT NULL DEFAULT 0,
    can_delete     INTEGER NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(principal_type, principal_id, resource_type, resource_id)
);

INSERT INTO rights_v2 SELECT * FROM rights;

DROP TABLE rights;
ALTER TABLE rights_v2 RENAME TO rights;

CREATE INDEX IF NOT EXISTS idx_rights_principal ON rights(principal_type, principal_id);
CREATE INDEX IF NOT EXISTS idx_rights_tenant    ON rights(tenant_id);

-- +goose Down
-- Note: perm_mode column removal and rights table narrowing are not supported
-- in SQLite without a full recreate. This down migration is intentionally omitted
-- to avoid data loss; roll forward instead.
SELECT 1;
```

- [ ] **Step 2: Verify the migration applies cleanly**

```bash
go test ./internal/db/... -v -run TestMigrate
```

Expected: PASS (the test opens an in-memory SQLite DB and runs all migrations)

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/006_permission_system.sql
git commit -m "feat(db): add perm_mode to tenants and extend rights principal/resource types"
```

---

## Task 2: Update `Can()` — add PermMode and ancestor inheritance

**Files:**
- Modify: `internal/auth/rbac.go`
- Modify: `internal/auth/rbac_test.go`

**Context:** `rbac.go` currently has `Can(p Principal, rights []Right, action Action, resourceType ResourceType, resourceID string) bool`. The existing tests call it with this 5-arg signature. We replace it completely — old tests must be updated to pass the new signature.

- [ ] **Step 1: Write the failing tests first**

Replace the entire contents of `internal/auth/rbac_test.go` with:

```go
package auth

import "testing"

func adminPrincipal() Principal {
	return Principal{ID: "admin-1", TenantID: "tenant-1", Username: "admin", UserType: UserTypeAdmin}
}

func superPrincipal() Principal {
	return Principal{ID: "super-1", TenantID: "tenant-1", Username: "super", UserType: UserTypeSuperAdmin}
}

func userPrincipal() Principal {
	return Principal{ID: "user-1", TenantID: "tenant-1", Username: "alice", UserType: UserTypeUser}
}

// ─── Admin bypass ─────────────────────────────────────────────────────────────

func TestCan_AdminAlwaysPermitted(t *testing.T) {
	for _, action := range []Action{ActionCreate, ActionRead, ActionUpdate, ActionDelete} {
		if !Can(adminPrincipal(), nil, PermModeExplicit, action, ResourceProject, "proj-1") {
			t.Errorf("admin should always be permitted for action %q", action)
		}
	}
}

func TestCan_SuperAdminAlwaysPermitted(t *testing.T) {
	if !Can(superPrincipal(), nil, PermModeExplicit, ActionDelete, ResourceBucket, "b-1") {
		t.Error("superadmin should always be permitted")
	}
}

func TestCan_AdminWithNoRights(t *testing.T) {
	if !Can(adminPrincipal(), []Right{}, PermModeExplicit, ActionDelete, ResourceBucket, "b-xyz") {
		t.Error("admin with empty rights should still be permitted")
	}
}

// ─── Explicit mode ────────────────────────────────────────────────────────────

func TestCan_Explicit_ExactMatch(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "project", ResourceID: "proj-1", CanRead: true}}
	if !Can(p, rights, PermModeExplicit, ActionRead, ResourceProject, "proj-1") {
		t.Error("exact match should be permitted in explicit mode")
	}
}

func TestCan_Explicit_WildcardMatch(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "bucket", ResourceID: "*", CanRead: true, CanCreate: true}}
	if !Can(p, rights, PermModeExplicit, ActionRead, ResourceBucket, "any-bucket") {
		t.Error("wildcard should grant read in explicit mode")
	}
	if !Can(p, rights, PermModeExplicit, ActionCreate, ResourceBucket, "another-bucket") {
		t.Error("wildcard should grant create in explicit mode")
	}
}

func TestCan_Explicit_NoMatch_Denied(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "project", ResourceID: "proj-1", CanRead: true}}
	// Wrong resource ID.
	if Can(p, rights, PermModeExplicit, ActionRead, ResourceProject, "proj-999") {
		t.Error("wrong resource ID should be denied")
	}
	// Wrong action.
	if Can(p, rights, PermModeExplicit, ActionDelete, ResourceProject, "proj-1") {
		t.Error("action not in rights should be denied")
	}
	// Wrong resource type.
	if Can(p, rights, PermModeExplicit, ActionRead, ResourceBucket, "proj-1") {
		t.Error("wrong resource type should be denied")
	}
}

func TestCan_Explicit_EmptyRights_Denied(t *testing.T) {
	if Can(userPrincipal(), []Right{}, PermModeExplicit, ActionRead, ResourceProject, "proj-1") {
		t.Error("empty rights in explicit mode should deny")
	}
}

func TestCan_Explicit_NoAncestorFallback(t *testing.T) {
	p := userPrincipal()
	// Grant on project — should NOT grant bucket access in explicit mode.
	rights := []Right{{ResourceType: "project", ResourceID: "proj-1", CanRead: true}}
	ancestor := ResourceAncestor{Type: ResourceProject, ID: "proj-1"}
	if Can(p, rights, PermModeExplicit, ActionRead, ResourceBucket, "bucket-1", ancestor) {
		t.Error("explicit mode should not fall back to ancestor grants")
	}
}

func TestCan_Explicit_WildcardDoesNotCrossResourceType(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "project", ResourceID: "*", CanRead: true}}
	if Can(p, rights, PermModeExplicit, ActionRead, ResourceBucket, "any-bucket") {
		t.Error("wildcard on projects should not grant bucket access")
	}
}

// ─── Inherit mode ─────────────────────────────────────────────────────────────

func TestCan_Inherit_AncestorGrantAllowed(t *testing.T) {
	p := userPrincipal()
	// Grant on project — should cascade to bucket in inherit mode.
	rights := []Right{{ResourceType: "project", ResourceID: "proj-1", CanRead: true}}
	ancestor := ResourceAncestor{Type: ResourceProject, ID: "proj-1"}
	if !Can(p, rights, PermModeInherit, ActionRead, ResourceBucket, "bucket-1", ancestor) {
		t.Error("inherit mode should fall back to ancestor grant")
	}
}

func TestCan_Inherit_SpecificOverridesAncestor(t *testing.T) {
	p := userPrincipal()
	rights := []Right{
		{ResourceType: "project", ResourceID: "proj-1", CanRead: true, CanDelete: true},
		{ResourceType: "bucket", ResourceID: "bucket-1", CanRead: true}, // no CanDelete
	}
	ancestor := ResourceAncestor{Type: ResourceProject, ID: "proj-1"}
	// Read is granted at bucket level — allowed.
	if !Can(p, rights, PermModeInherit, ActionRead, ResourceBucket, "bucket-1", ancestor) {
		t.Error("bucket-level read should be permitted")
	}
	// Delete is NOT in bucket right (even though it's in project) — denied because
	// bucket-level right exists and doesn't grant delete.
	if Can(p, rights, PermModeInherit, ActionDelete, ResourceBucket, "bucket-1", ancestor) {
		t.Error("delete should be denied when bucket right exists but doesn't grant delete")
	}
}

func TestCan_Inherit_NoGrantAnywhere_Denied(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "project", ResourceID: "proj-2", CanRead: true}}
	ancestor := ResourceAncestor{Type: ResourceProject, ID: "proj-1"} // different project
	if Can(p, rights, PermModeInherit, ActionRead, ResourceBucket, "bucket-1", ancestor) {
		t.Error("no grant anywhere should deny in inherit mode")
	}
}

// ─── Open mode ────────────────────────────────────────────────────────────────

func TestCan_Open_NoRightExists_Allowed(t *testing.T) {
	p := userPrincipal()
	// No rights at all — open mode defaults to allowed.
	if !Can(p, []Right{}, PermModeOpen, ActionRead, ResourceProject, "proj-1") {
		t.Error("open mode with no rights should allow by default")
	}
}

func TestCan_Open_RightExistsAndGrantsAction_Allowed(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "project", ResourceID: "proj-1", CanRead: true}}
	if !Can(p, rights, PermModeOpen, ActionRead, ResourceProject, "proj-1") {
		t.Error("open mode with explicit grant should allow")
	}
}

func TestCan_Open_RightExistsButNotAction_Denied(t *testing.T) {
	p := userPrincipal()
	// Right exists for proj-1 but only grants read, not delete.
	rights := []Right{{ResourceType: "project", ResourceID: "proj-1", CanRead: true}}
	if Can(p, rights, PermModeOpen, ActionDelete, ResourceProject, "proj-1") {
		t.Error("open mode: right exists for resource but action not granted — should deny")
	}
}

func TestCan_Open_UnrelatedRightDoesNotRestrict(t *testing.T) {
	p := userPrincipal()
	// Right exists for proj-2 — proj-1 has no right, so open mode allows proj-1.
	rights := []Right{{ResourceType: "project", ResourceID: "proj-2", CanRead: true}}
	if !Can(p, rights, PermModeOpen, ActionDelete, ResourceProject, "proj-1") {
		t.Error("open mode: unrelated right should not restrict access to other resource")
	}
}

// ─── All actions ──────────────────────────────────────────────────────────────

func TestCan_AllActions(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "tenant", ResourceID: "tenant-1", CanCreate: true, CanRead: true, CanUpdate: true, CanDelete: true}}
	for _, action := range []Action{ActionCreate, ActionRead, ActionUpdate, ActionDelete} {
		if !Can(p, rights, PermModeExplicit, action, ResourceTenant, "tenant-1") {
			t.Errorf("full rights should permit action %q", action)
		}
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
go test ./internal/auth/... -run TestCan -v 2>&1 | head -40
```

Expected: compilation error or FAIL (new signature not yet implemented)

- [ ] **Step 3: Update `internal/auth/rbac.go`**

Replace the entire file:

```go
package auth

// PermMode controls how tenant-wide access is evaluated for principals with no
// explicit right on a given resource.
type PermMode string

const (
	// PermModeExplicit (default): no access unless a right is explicitly granted.
	PermModeExplicit PermMode = "explicit"
	// PermModeOpen: full access unless a right exists and does not grant the action.
	PermModeOpen PermMode = "open"
	// PermModeInherit: like explicit, but a grant on a parent resource cascades
	// to all children (bucket inherits from project, document from bucket, etc.).
	PermModeInherit PermMode = "inherit"
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
	ResourceTenant   ResourceType = "tenant"
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
// Admin and superadmin principals are always permitted regardless of rights or mode.
//
// For regular users, rights is the slice returned by Store.GetUserRights or
// Store.GetAPIKeyRights. mode controls the default stance when no right is found:
//   - PermModeExplicit: denied unless an exact or wildcard right grants the action.
//   - PermModeOpen: allowed unless a right exists for this resource and doesn't
//     grant the action.
//   - PermModeInherit: like explicit but if no right is found at the target level,
//     the check walks up through ancestors (nearest first).
//
// ancestors is ordered nearest → furthest (e.g. bucket, then project, then tenant).
func Can(p Principal, rights []Right, mode PermMode, action Action, resourceType ResourceType, resourceID string, ancestors ...ResourceAncestor) bool {
	if p.IsAdmin() {
		return true
	}

	switch mode {
	case PermModeOpen:
		return canOpen(rights, action, resourceType, resourceID)
	case PermModeInherit:
		return canInherit(rights, action, resourceType, resourceID, ancestors)
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
		// A matching right exists — honour its action bits (opt-down).
		return rightGrantsAction(r, action)
	}
	// No right for this resource — open by default.
	return true
}

// canInherit checks the target resource, then walks up ancestors until a right
// is found. If no right exists anywhere in the chain, access is denied.
func canInherit(rights []Right, action Action, resourceType ResourceType, resourceID string, ancestors []ResourceAncestor) bool {
	if checkRights(rights, action, resourceType, resourceID) {
		return true
	}
	// Walk ancestors nearest → furthest.
	for _, anc := range ancestors {
		if checkRights(rights, action, anc.Type, anc.ID) {
			return true
		}
	}
	return false
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

- [ ] **Step 4: Run tests — verify they pass**

```bash
go test ./internal/auth/... -run TestCan -v
```

Expected: all `TestCan_*` tests PASS

- [ ] **Step 5: Run the full auth test suite**

```bash
go test ./internal/auth/... -v
```

Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/auth/rbac.go internal/auth/rbac_test.go
git commit -m "feat(auth): add PermMode and ancestor inheritance to Can()"
```

---

## Task 3: Store layer — new rights and perm_mode methods

**Files:**
- Modify: `internal/auth/store.go`
- Modify: `internal/auth/store_test.go`

**Context:** `store_test.go` uses `newTestStore(t)` which returns `*Store`. Look at `internal/auth/main_test.go` for the test package setup — it's `package auth` (white-box). The `db.Open` and `db.Migrate` helpers are available. The existing `newTestStore` helper creates an in-memory SQLite DB and runs migrations.

- [ ] **Step 1: Read the existing `store_test.go` to understand `newTestStore`**

```bash
head -60 internal/auth/store_test.go
```

- [ ] **Step 2: Write the failing tests for the six new store methods**

Add the following to the **end** of `internal/auth/store_test.go`:

```go
// ─── Rights store methods ─────────────────────────────────────────────────────

func TestUpsertRight_CreateAndUpdate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tenant := seedTenant(t, s)
	user := seedUser(t, s, tenant.ID, "alice", UserTypeUser)

	params := UpsertRightParams{
		TenantID:      tenant.ID,
		PrincipalType: "user",
		PrincipalID:   user.ID,
		ResourceType:  "project",
		ResourceID:    "proj-1",
		CanRead:       true,
	}
	if err := s.UpsertRight(ctx, params); err != nil {
		t.Fatalf("UpsertRight: %v", err)
	}

	rights, err := s.ListRights(ctx, tenant.ID, "user", user.ID)
	if err != nil {
		t.Fatalf("ListRights: %v", err)
	}
	if len(rights) != 1 {
		t.Fatalf("expected 1 right, got %d", len(rights))
	}
	if !rights[0].CanRead {
		t.Error("CanRead should be true")
	}
	if rights[0].CanDelete {
		t.Error("CanDelete should be false")
	}

	// Update: grant delete too.
	params.CanDelete = true
	if err := s.UpsertRight(ctx, params); err != nil {
		t.Fatalf("UpsertRight update: %v", err)
	}
	rights2, _ := s.ListRights(ctx, tenant.ID, "user", user.ID)
	if !rights2[0].CanDelete {
		t.Error("CanDelete should be true after update")
	}
}

func TestDeleteRight_RemovesRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tenant := seedTenant(t, s)
	user := seedUser(t, s, tenant.ID, "bob", UserTypeUser)

	params := UpsertRightParams{
		TenantID:      tenant.ID,
		PrincipalType: "user",
		PrincipalID:   user.ID,
		ResourceType:  "bucket",
		ResourceID:    "bucket-1",
		CanRead:       true,
	}
	_ = s.UpsertRight(ctx, params)

	if err := s.DeleteRight(ctx, tenant.ID, "user", user.ID, "bucket", "bucket-1"); err != nil {
		t.Fatalf("DeleteRight: %v", err)
	}

	rights, _ := s.ListRights(ctx, tenant.ID, "user", user.ID)
	if len(rights) != 0 {
		t.Errorf("expected 0 rights after delete, got %d", len(rights))
	}
}

func TestDeleteRight_NotFound_ReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := seedTenant(t, s)

	err := s.DeleteRight(ctx, tenant.ID, "user", "no-user", "project", "no-proj")
	if err == nil {
		t.Error("expected error deleting non-existent right")
	}
}

func TestListRights_IsolatesByPrincipal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tenant := seedTenant(t, s)
	alice := seedUser(t, s, tenant.ID, "alice", UserTypeUser)
	bob := seedUser(t, s, tenant.ID, "bob", UserTypeUser)

	_ = s.UpsertRight(ctx, UpsertRightParams{
		TenantID: tenant.ID, PrincipalType: "user", PrincipalID: alice.ID,
		ResourceType: "project", ResourceID: "proj-1", CanRead: true,
	})
	_ = s.UpsertRight(ctx, UpsertRightParams{
		TenantID: tenant.ID, PrincipalType: "user", PrincipalID: bob.ID,
		ResourceType: "project", ResourceID: "proj-2", CanRead: true,
	})

	aliceRights, _ := s.ListRights(ctx, tenant.ID, "user", alice.ID)
	if len(aliceRights) != 1 || aliceRights[0].ResourceID != "proj-1" {
		t.Errorf("alice should have exactly 1 right on proj-1, got %+v", aliceRights)
	}

	bobRights, _ := s.ListRights(ctx, tenant.ID, "user", bob.ID)
	if len(bobRights) != 1 || bobRights[0].ResourceID != "proj-2" {
		t.Errorf("bob should have exactly 1 right on proj-2, got %+v", bobRights)
	}
}

func TestGetAPIKeyRights_ReturnOnlyAPIKeyRights(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tenant := seedTenant(t, s)
	user := seedUser(t, s, tenant.ID, "alice", UserTypeUser)

	// Grant a right to the user (principal_type=user).
	_ = s.UpsertRight(ctx, UpsertRightParams{
		TenantID: tenant.ID, PrincipalType: "user", PrincipalID: user.ID,
		ResourceType: "project", ResourceID: "proj-1", CanRead: true,
	})

	// Grant a right to an API key (principal_type=apikey).
	keyID := "apikey-test-id"
	_ = s.UpsertRight(ctx, UpsertRightParams{
		TenantID: tenant.ID, PrincipalType: "apikey", PrincipalID: keyID,
		ResourceType: "bucket", ResourceID: "bucket-1", CanRead: true,
	})

	rights, err := s.GetAPIKeyRights(ctx, tenant.ID, keyID)
	if err != nil {
		t.Fatalf("GetAPIKeyRights: %v", err)
	}
	if len(rights) != 1 {
		t.Fatalf("expected 1 apikey right, got %d", len(rights))
	}
	if rights[0].ResourceType != "bucket" {
		t.Errorf("expected bucket right, got %s", rights[0].ResourceType)
	}
}

func TestGetSetTenantPermMode(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tenant := seedTenant(t, s)

	// Default should be explicit.
	mode, err := s.GetTenantPermMode(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("GetTenantPermMode: %v", err)
	}
	if mode != PermModeExplicit {
		t.Errorf("default mode: got %q, want %q", mode, PermModeExplicit)
	}

	// Update to inherit.
	if err := s.SetTenantPermMode(ctx, tenant.ID, PermModeInherit); err != nil {
		t.Fatalf("SetTenantPermMode: %v", err)
	}

	mode2, _ := s.GetTenantPermMode(ctx, tenant.ID)
	if mode2 != PermModeInherit {
		t.Errorf("after set: got %q, want %q", mode2, PermModeInherit)
	}
}

func TestGetTenantPermMode_UnknownTenantReturnsExplicit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mode, err := s.GetTenantPermMode(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mode != PermModeExplicit {
		t.Errorf("unknown tenant: got %q, want explicit", mode)
	}
}
```

- [ ] **Step 3: Run the new tests — verify they fail**

```bash
go test ./internal/auth/... -run "TestUpsertRight|TestDeleteRight|TestListRights|TestGetAPIKeyRights|TestGetSetTenantPermMode|TestGetTenantPermMode_Unknown" -v 2>&1 | head -30
```

Expected: compilation error (methods not yet defined)

- [ ] **Step 4: Add the six new methods to `internal/auth/store.go`**

Add the following to the `─── RBAC ───` section of `store.go`, after the existing `GetUserRights` method:

```go
// UpsertRightParams holds the fields for creating or updating a right row.
type UpsertRightParams struct {
	TenantID      string
	PrincipalType string // "user" | "group" | "apikey"
	PrincipalID   string
	ResourceType  string // "tenant" | "project" | "bucket" | "document"
	ResourceID    string // specific UUID or "*"
	CanCreate     bool
	CanRead       bool
	CanUpdate     bool
	CanDelete     bool
}

// UpsertRight creates or replaces a right row for the given principal/resource.
func (s *Store) UpsertRight(ctx context.Context, p UpsertRightParams) error {
	id := uuid.New().String()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rights
		    (id, tenant_id, principal_type, principal_id, resource_type, resource_id,
		     can_create, can_read, can_update, can_delete)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(principal_type, principal_id, resource_type, resource_id)
		DO UPDATE SET
		    can_create = excluded.can_create,
		    can_read   = excluded.can_read,
		    can_update = excluded.can_update,
		    can_delete = excluded.can_delete`,
		id, p.TenantID, p.PrincipalType, p.PrincipalID,
		p.ResourceType, p.ResourceID,
		boolToInt(p.CanCreate), boolToInt(p.CanRead),
		boolToInt(p.CanUpdate), boolToInt(p.CanDelete),
	)
	if err != nil {
		return fmt.Errorf("upsert right: %w", err)
	}
	return nil
}

// DeleteRight removes the specific right row. Returns an error if the row does
// not exist.
func (s *Store) DeleteRight(ctx context.Context, tenantID, principalType, principalID, resourceType, resourceID string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM rights
		WHERE tenant_id = ? AND principal_type = ? AND principal_id = ?
		  AND resource_type = ? AND resource_id = ?`,
		tenantID, principalType, principalID, resourceType, resourceID,
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

// ListRights returns all rights for a given principal, ordered by resource_type,
// resource_id.
func (s *Store) ListRights(ctx context.Context, tenantID, principalType, principalID string) ([]Right, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT principal_type, principal_id, resource_type, resource_id,
		       can_create, can_read, can_update, can_delete
		FROM rights
		WHERE tenant_id = ? AND principal_type = ? AND principal_id = ?
		ORDER BY resource_type, resource_id`,
		tenantID, principalType, principalID,
	)
	if err != nil {
		return nil, fmt.Errorf("list rights: %w", err)
	}
	defer rows.Close()
	return scanRights(rows)
}

// GetAPIKeyRights returns all rights for an API key principal.
func (s *Store) GetAPIKeyRights(ctx context.Context, tenantID, apiKeyID string) ([]Right, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT principal_type, principal_id, resource_type, resource_id,
		       can_create, can_read, can_update, can_delete
		FROM rights
		WHERE tenant_id = ? AND principal_type = 'apikey' AND principal_id = ?`,
		tenantID, apiKeyID,
	)
	if err != nil {
		return nil, fmt.Errorf("get api key rights: %w", err)
	}
	defer rows.Close()
	return scanRights(rows)
}

// GetTenantPermMode fetches the perm_mode for the given tenant. Returns
// PermModeExplicit if the tenant does not exist (fail-safe).
func (s *Store) GetTenantPermMode(ctx context.Context, tenantID string) (PermMode, error) {
	var mode string
	err := s.db.QueryRowContext(ctx,
		`SELECT perm_mode FROM tenants WHERE id = ? AND deleted_at IS NULL`, tenantID,
	).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return PermModeExplicit, nil
	}
	if err != nil {
		return PermModeExplicit, fmt.Errorf("get perm mode: %w", err)
	}
	return PermMode(mode), nil
}

// SetTenantPermMode updates the perm_mode for a tenant.
func (s *Store) SetTenantPermMode(ctx context.Context, tenantID string, mode PermMode) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE tenants SET perm_mode = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND deleted_at IS NULL`,
		string(mode), tenantID,
	)
	if err != nil {
		return fmt.Errorf("set perm mode: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("tenant not found")
	}
	return nil
}
```

Also add these two helpers at the bottom of `store.go` (before the closing brace of the file, after `scanUser`):

```go
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func scanRights(rows *sql.Rows) ([]Right, error) {
	var rights []Right
	for rows.Next() {
		var r Right
		var cc, cr, cu, cd int
		if err := rows.Scan(
			&r.PrincipalType, &r.PrincipalID,
			&r.ResourceType, &r.ResourceID,
			&cc, &cr, &cu, &cd,
		); err != nil {
			return nil, fmt.Errorf("scan right: %w", err)
		}
		r.CanCreate = cc == 1
		r.CanRead = cr == 1
		r.CanUpdate = cu == 1
		r.CanDelete = cd == 1
		rights = append(rights, r)
	}
	return rights, rows.Err()
}
```

- [ ] **Step 5: Run the new tests — verify they pass**

```bash
go test ./internal/auth/... -run "TestUpsertRight|TestDeleteRight|TestListRights|TestGetAPIKeyRights|TestGetSetTenantPermMode|TestGetTenantPermMode_Unknown" -v
```

Expected: all tests PASS

- [ ] **Step 6: Run the full auth suite**

```bash
go test ./internal/auth/... -v
```

Expected: all tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/auth/store.go internal/auth/store_test.go
git commit -m "feat(auth): add UpsertRight, DeleteRight, ListRights, GetAPIKeyRights, GetTenantPermMode, SetTenantPermMode"
```

---

## Task 4: API middleware — RightsCtx and CanMiddleware

**Files:**
- Modify: `internal/api/context.go`

**Context:** `context.go` uses `ctxKey` string type. `TenantCtx` currently loads only the tenant struct. `auth.PrincipalFromContext` is available. `auth.AuthMethodAPIKey` identifies API key auth.

- [ ] **Step 1: Write the failing tests**

Create `internal/api/security_test.go`:

```go
package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"tinydm/internal/auth"
)

// seedRegularUser creates a regular (non-admin) user in the given tenant and
// returns a Bearer token for that user.
func (ts *testServer) seedRegularUser(t *testing.T, tenantID, username, password string) string {
	t.Helper()
	ctx := context.Background()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	_, err = ts.authStore.CreateUser(ctx, tenantID, username, username+"@test.local", "Test", "User", hash, auth.UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return ts.login(t, tenantID, username, password)
}

func TestPermissions_RegularUser_DeniedWithoutGrant(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedAdminUser(t, "Acme", "admin", "pass")
	adminToken := ts.login(t, tenant.ID, "admin", "pass")
	userToken := ts.seedRegularUser(t, tenant.ID, "alice", "pass")

	// Admin creates a project.
	var proj map[string]any
	ts.doJSON(t, http.MethodPost,
		fmt.Sprintf("/api/v1/tenants/%s/projects", tenant.ID),
		map[string]string{"name": "Alpha"}, bearer(adminToken), &proj)
	projID := proj["id"].(string)

	// Regular user must be denied listing projects (no right granted).
	resp := ts.doJSON(t, http.MethodGet,
		fmt.Sprintf("/api/v1/tenants/%s/projects", tenant.ID),
		nil, bearer(userToken), nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusForbidden)

	// Regular user must be denied creating a project.
	resp2 := ts.doJSON(t, http.MethodPost,
		fmt.Sprintf("/api/v1/tenants/%s/projects", tenant.ID),
		map[string]string{"name": "Sneaky"}, bearer(userToken), nil)
	defer resp2.Body.Close()
	assertStatus(t, resp2, http.StatusForbidden)

	_ = projID
}

func TestPermissions_RegularUser_AllowedAfterGrant(t *testing.T) {
	ts := newTestServer(t)
	tenant, user := ts.seedAdminUser(t, "Acme", "admin", "pass")
	adminToken := ts.login(t, tenant.ID, "admin", "pass")
	alice, _ := ts.authStore.CreateUser(context.Background(), tenant.ID, "alice", "alice@test.local", "Alice", "Smith", mustHash(t, "pass"), auth.UserTypeUser)
	aliceToken := ts.login(t, tenant.ID, "alice", "pass")

	// Admin creates a project.
	var proj map[string]any
	ts.doJSON(t, http.MethodPost,
		fmt.Sprintf("/api/v1/tenants/%s/projects", tenant.ID),
		map[string]string{"name": "Alpha"}, bearer(adminToken), &proj)
	projID := proj["id"].(string)

	// Grant alice read on projects (wildcard).
	err := ts.authStore.UpsertRight(context.Background(), auth.UpsertRightParams{
		TenantID: tenant.ID, PrincipalType: "user", PrincipalID: alice.ID,
		ResourceType: "project", ResourceID: "*", CanRead: true,
	})
	if err != nil {
		t.Fatalf("UpsertRight: %v", err)
	}

	// Alice should now be able to list projects.
	var list map[string]any
	resp := ts.doJSON(t, http.MethodGet,
		fmt.Sprintf("/api/v1/tenants/%s/projects", tenant.ID),
		nil, bearer(aliceToken), &list)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	_ = user
	_ = projID
}

func TestPermissions_AdminBypassesRights(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedAdminUser(t, "Acme", "admin", "pass")
	adminToken := ts.login(t, tenant.ID, "admin", "pass")

	// Admin can list projects with no rights configured.
	resp := ts.doJSON(t, http.MethodGet,
		fmt.Sprintf("/api/v1/tenants/%s/projects", tenant.ID),
		nil, bearer(adminToken), nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)
}

func TestPermissions_InheritMode_BucketGrantFromProject(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedAdminUser(t, "Acme", "admin", "pass")
	adminToken := ts.login(t, tenant.ID, "admin", "pass")

	alice, _ := ts.authStore.CreateUser(context.Background(), tenant.ID, "alice", "alice@test.local", "Alice", "Smith", mustHash(t, "pass"), auth.UserTypeUser)
	aliceToken := ts.login(t, tenant.ID, "alice", "pass")

	// Admin creates a project and a bucket.
	var proj map[string]any
	ts.doJSON(t, http.MethodPost,
		fmt.Sprintf("/api/v1/tenants/%s/projects", tenant.ID),
		map[string]string{"name": "Alpha"}, bearer(adminToken), &proj)
	projID := proj["id"].(string)

	var bucket map[string]any
	ts.doJSON(t, http.MethodPost,
		fmt.Sprintf("/api/v1/tenants/%s/projects/%s/buckets", tenant.ID, projID),
		map[string]string{"name": "uploads"}, bearer(adminToken), &bucket)
	bucketID := bucket["id"].(string)

	// Switch tenant to inherit mode.
	if err := ts.authStore.SetTenantPermMode(context.Background(), tenant.ID, auth.PermModeInherit); err != nil {
		t.Fatalf("SetTenantPermMode: %v", err)
	}

	// Grant alice read on the project (no bucket-level right).
	_ = ts.authStore.UpsertRight(context.Background(), auth.UpsertRightParams{
		TenantID: tenant.ID, PrincipalType: "user", PrincipalID: alice.ID,
		ResourceType: "project", ResourceID: projID, CanRead: true,
	})

	// Alice should be able to read the bucket (inherited from project).
	resp := ts.doJSON(t, http.MethodGet,
		fmt.Sprintf("/api/v1/tenants/%s/projects/%s/buckets/%s", tenant.ID, projID, bucketID),
		nil, bearer(aliceToken), nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)
}

// mustHash is a test helper that hashes a password and fails the test on error.
func mustHash(t *testing.T, password string) string {
	t.Helper()
	h, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	return h
}
```

- [ ] **Step 2: Run the tests — verify they fail**

```bash
go test ./internal/api/... -run "TestPermissions" -v 2>&1 | head -40
```

Expected: FAIL — regular users currently pass where they should get 403

- [ ] **Step 3: Extend `internal/api/context.go`**

Add new context keys, extend `TenantCtx`, and add `RightsCtx` + `CanMiddleware`:

```go
// Add to the const block after existing keys:
const (
	tenantKey   ctxKey = "tenant"
	projectKey  ctxKey = "project"
	bucketKey   ctxKey = "bucket"
	documentKey ctxKey = "document"
	versionKey  ctxKey = "version"
	rightsKey   ctxKey = "rights"
	permModeKey ctxKey = "permMode"
)
```

Replace the existing `TenantCtx` function with:

```go
// TenantCtx loads the tenant identified by {tenantID} into the context,
// and also stores its perm_mode for use by RightsCtx and CanMiddleware.
func TenantCtx(repoStore *repo.Store, authStore *auth.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "tenantID")
			tenant, err := repoStore.GetTenant(r.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if tenant == nil {
				writeError(w, http.StatusNotFound, "tenant not found")
				return
			}
			mode, err := authStore.GetTenantPermMode(r.Context(), id)
			if err != nil {
				mode = auth.PermModeExplicit // fail-safe
			}
			ctx := contextWith(r.Context(), tenantKey, tenant)
			ctx = contextWith(ctx, permModeKey, mode)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
```

Add after `TenantCtx`:

```go
// RightsCtx loads the authenticated principal's rights into the context.
// It must run after RequireAuth and TenantCtx.
func RightsCtx(authStore *auth.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.PrincipalFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			// Admins bypass rights checks — no need to load rights.
			if p.IsAdmin() {
				next.ServeHTTP(w, r)
				return
			}
			tenant := tenantFromCtx(r)
			if tenant == nil {
				next.ServeHTTP(w, r)
				return
			}
			var rights []auth.Right
			var err error
			if p.AuthMethod == auth.AuthMethodAPIKey {
				rights, err = authStore.GetAPIKeyRights(r.Context(), tenant.ID, p.ID)
			} else {
				rights, err = authStore.GetUserRights(r.Context(), tenant.ID, p.ID)
			}
			if err != nil {
				// Fail-safe: no rights loaded means all Can() checks deny.
				rights = nil
			}
			ctx := contextWith(r.Context(), rightsKey, rights)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CanMiddleware returns middleware that checks whether the authenticated
// principal may perform action on the given resource. getResourceID and
// getAncestors are called at request time to extract the resource ID and
// ancestor chain from URL params and context.
func CanMiddleware(
	authStore *auth.Store,
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
			// Admins bypass all permission checks.
			if p.IsAdmin() {
				next.ServeHTTP(w, r)
				return
			}

			mode, _ := r.Context().Value(permModeKey).(auth.PermMode)
			if mode == "" {
				mode = auth.PermModeExplicit
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

Add context accessors after existing ones:

```go
func rightsFromCtx(r *http.Request) []auth.Right {
	v, _ := r.Context().Value(rightsKey).([]auth.Right)
	return v
}

func permModeFromCtx(r *http.Request) auth.PermMode {
	v, _ := r.Context().Value(permModeKey).(auth.PermMode)
	if v == "" {
		return auth.PermModeExplicit
	}
	return v
}
```

- [ ] **Step 4: Update `TenantCtx` call sites in `routes.go`**

`TenantCtx` now takes two arguments. In `internal/api/routes.go`, update:

```go
r.Use(TenantCtx(repoStore))
```

to:

```go
r.Use(TenantCtx(repoStore, authStore))
```

- [ ] **Step 5: Add `RightsCtx` to the authenticated route group in `routes.go`**

In the authenticated `r.Group` block, after the existing `r.Use(audit.Middleware(auditStore))` line, add:

```go
r.Use(RightsCtx(authStore))
```

- [ ] **Step 6: Replace `RequireAdmin` with `CanMiddleware` on project/bucket/document routes**

In `internal/api/routes.go`, replace the project/bucket/document route block (lines 75-129) with:

```go
			// Projects
			r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceProject,
				nil, nil)).Get("/projects", projectHandler.List)
			r.With(CanMiddleware(authStore, auth.ActionCreate, auth.ResourceProject,
				nil, nil)).Post("/projects", projectHandler.Create)

			r.Route("/projects/{projectID}", func(r chi.Router) {
				r.Use(ProjectCtx(repoStore))

				r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceProject,
					func(r *http.Request) string { return chi.URLParam(r, "projectID") },
					nil)).Get("/", projectHandler.Get)
				r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceProject,
					func(r *http.Request) string { return chi.URLParam(r, "projectID") },
					nil)).Put("/", projectHandler.Update)
				r.With(CanMiddleware(authStore, auth.ActionDelete, auth.ResourceProject,
					func(r *http.Request) string { return chi.URLParam(r, "projectID") },
					nil)).Delete("/", projectHandler.Delete)

				// Buckets
				r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceBucket,
					nil,
					func(r *http.Request) []auth.ResourceAncestor {
						return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
					})).Get("/buckets", bucketHandler.List)
				r.With(CanMiddleware(authStore, auth.ActionCreate, auth.ResourceBucket,
					nil,
					func(r *http.Request) []auth.ResourceAncestor {
						return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
					})).Post("/buckets", bucketHandler.Create)

				r.Route("/buckets/{bucketID}", func(r chi.Router) {
					r.Use(BucketCtx(repoStore))

					r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceBucket,
						func(r *http.Request) string { return chi.URLParam(r, "bucketID") },
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
						})).Get("/", bucketHandler.Get)
					r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceBucket,
						func(r *http.Request) string { return chi.URLParam(r, "bucketID") },
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
						})).Put("/", bucketHandler.Update)
					r.With(CanMiddleware(authStore, auth.ActionDelete, auth.ResourceBucket,
						func(r *http.Request) string { return chi.URLParam(r, "bucketID") },
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")}}
						})).Delete("/", bucketHandler.Delete)

					// Documents
					r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceDocument,
						nil,
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{
								{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
								{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
							}
						})).Get("/documents", docHandler.List)
					r.With(CanMiddleware(authStore, auth.ActionCreate, auth.ResourceDocument,
						nil,
						func(r *http.Request) []auth.ResourceAncestor {
							return []auth.ResourceAncestor{
								{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
								{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
							}
						})).Post("/documents", docHandler.Upload)

					r.Route("/documents/{documentID}", func(r chi.Router) {
						r.Use(DocumentCtx(repoStore))

						r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/", docHandler.Get)
						r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Put("/", docHandler.Update)
						r.With(CanMiddleware(authStore, auth.ActionDelete, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Delete("/", docHandler.Delete)
						r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/content", docHandler.Download)

						// Versions
						r.With(CanMiddleware(authStore, auth.ActionRead, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Get("/versions", docHandler.ListVersions)
						r.Route("/versions/{versionID}", func(r chi.Router) {
							r.Use(VersionCtx(repoStore))
							r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
								func(r *http.Request) string { return chi.URLParam(r, "documentID") },
								func(r *http.Request) []auth.ResourceAncestor {
									return []auth.ResourceAncestor{
										{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
										{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
									}
								})).Post("/restore", docHandler.RestoreVersion)
						})

						// Tags and properties — require document read at minimum
						r.Get("/tags", tagHandler.List)
						r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Put("/tags", tagHandler.Replace)
						r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Post("/tags/{tag}", tagHandler.Add)
						r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Delete("/tags/{tag}", tagHandler.Remove)

						r.Get("/properties", propHandler.List)
						r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Put("/properties", propHandler.Replace)
						r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
							func(r *http.Request) string { return chi.URLParam(r, "documentID") },
							func(r *http.Request) []auth.ResourceAncestor {
								return []auth.ResourceAncestor{
									{Type: auth.ResourceBucket, ID: chi.URLParam(r, "bucketID")},
									{Type: auth.ResourceProject, ID: chi.URLParam(r, "projectID")},
								}
							})).Put("/properties/{key}", propHandler.Set)
						r.With(CanMiddleware(authStore, auth.ActionUpdate, auth.ResourceDocument,
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
```

- [ ] **Step 7: Compile check**

```bash
go build ./internal/api/...
```

Expected: no errors

- [ ] **Step 8: Run the permission integration tests**

```bash
go test ./internal/api/... -run "TestPermissions" -v
```

Expected: all `TestPermissions_*` tests PASS

- [ ] **Step 9: Run the full API test suite**

```bash
go test ./internal/api/... -v
```

Expected: all tests PASS (existing admin/superadmin tests unaffected — they bypass `CanMiddleware`)

- [ ] **Step 10: Commit**

```bash
git add internal/api/context.go internal/api/routes.go internal/api/security_test.go
git commit -m "feat(api): add RightsCtx and CanMiddleware; enforce RBAC on project/bucket/document routes"
```

---

## Task 5: Web UI — rights panel on user and API key pages, perm_mode selector

**Files:**
- Modify: `internal/web/handlers.go`
- Modify: `internal/web/web.go`
- Modify: `internal/web/templates/users.html`
- Modify: `internal/web/templates/apikeys.html`
- Modify: `internal/web/templates/tenants.html`

**Context:** Handlers in `web/handlers.go` use `h.auth.ListRights(...)` etc. The `render` and `renderPartial` helpers are available. HTMX is used throughout — buttons use `hx-post`, `hx-delete`, `hx-target` attributes. Templates use `{{define "content"}}` blocks. The `tenants.html` template must be checked/updated; look at it to understand what's already there before adding the perm_mode card.

- [ ] **Step 1: Read tenants.html to understand its current structure**

```bash
cat internal/web/templates/tenants.html
```

- [ ] **Step 2: Add new page data structs and handlers to `handlers.go`**

Add the following after the last handler in `internal/web/handlers.go`:

```go
// ── Rights page data ──────────────────────────────────────────────────────────

// WebRight wraps auth.Right with a display name for the resource.
type WebRight struct {
	auth.Right
	ResourceName string
}

type userRightsPage struct {
	basePage
	Tenant *repo.Tenant
	User   *auth.User
	Rights []auth.Right
}

type apiKeyRightsPage struct {
	basePage
	Tenant *repo.Tenant
	Key    *auth.APIKey
	Rights []auth.Right
}

// ── User rights handlers ──────────────────────────────────────────────────────

func (h *Handler) userRightsPanel(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	userID := chi.URLParam(r, "userID")

	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	user, err := h.auth.GetUserByID(r.Context(), userID)
	if err != nil || user == nil || user.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	rights, err := h.auth.ListRights(r.Context(), tenantID, "user", userID)
	if err != nil {
		rights = nil
	}

	data := userRightsPage{
		basePage: h.base(r, "users"),
		Tenant:   tenant,
		User:     user,
		Rights:   rights,
	}
	h.renderPartial(w, "users", "user-rights-panel", data)
}

func (h *Handler) addUserRight(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	userID := chi.URLParam(r, "userID")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	params := auth.UpsertRightParams{
		TenantID:      tenantID,
		PrincipalType: "user",
		PrincipalID:   userID,
		ResourceType:  r.FormValue("resource_type"),
		ResourceID:    r.FormValue("resource_id"),
		CanCreate:     r.FormValue("can_create") == "on",
		CanRead:       r.FormValue("can_read") == "on",
		CanUpdate:     r.FormValue("can_update") == "on",
		CanDelete:     r.FormValue("can_delete") == "on",
	}
	if params.ResourceID == "" {
		params.ResourceID = "*"
	}

	if err := h.auth.UpsertRight(r.Context(), params); err != nil {
		http.Error(w, "failed to add right", http.StatusInternalServerError)
		return
	}

	// Re-render the panel partial.
	rights, _ := h.auth.ListRights(r.Context(), tenantID, "user", userID)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	user, _ := h.auth.GetUserByID(r.Context(), userID)
	data := userRightsPage{basePage: h.base(r, "users"), Tenant: tenant, User: user, Rights: rights}
	h.renderPartial(w, "users", "user-rights-panel", data)
}

func (h *Handler) removeUserRight(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	userID := chi.URLParam(r, "userID")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	resourceType := r.FormValue("resource_type")
	resourceID := r.FormValue("resource_id")

	_ = h.auth.DeleteRight(r.Context(), tenantID, "user", userID, resourceType, resourceID)

	rights, _ := h.auth.ListRights(r.Context(), tenantID, "user", userID)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	user, _ := h.auth.GetUserByID(r.Context(), userID)
	data := userRightsPage{basePage: h.base(r, "users"), Tenant: tenant, User: user, Rights: rights}
	h.renderPartial(w, "users", "user-rights-panel", data)
}

// ── API key rights handlers ───────────────────────────────────────────────────

func (h *Handler) apiKeyRightsPanel(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	keyID := chi.URLParam(r, "keyID")

	tenant, err := h.repo.GetTenant(r.Context(), tenantID)
	if err != nil || tenant == nil {
		http.NotFound(w, r)
		return
	}
	keys, _, err := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var key *auth.APIKey
	for _, k := range keys {
		if k.ID == keyID {
			key = k
			break
		}
	}
	if key == nil {
		http.NotFound(w, r)
		return
	}
	rights, _ := h.auth.GetAPIKeyRights(r.Context(), tenantID, keyID)
	data := apiKeyRightsPage{basePage: h.base(r, "apikeys"), Tenant: tenant, Key: key, Rights: rights}
	h.renderPartial(w, "apikeys", "apikey-rights-panel", data)
}

func (h *Handler) addAPIKeyRight(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	keyID := chi.URLParam(r, "keyID")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	params := auth.UpsertRightParams{
		TenantID:      tenantID,
		PrincipalType: "apikey",
		PrincipalID:   keyID,
		ResourceType:  r.FormValue("resource_type"),
		ResourceID:    r.FormValue("resource_id"),
		CanCreate:     r.FormValue("can_create") == "on",
		CanRead:       r.FormValue("can_read") == "on",
		CanUpdate:     r.FormValue("can_update") == "on",
		CanDelete:     r.FormValue("can_delete") == "on",
	}
	if params.ResourceID == "" {
		params.ResourceID = "*"
	}
	_ = h.auth.UpsertRight(r.Context(), params)

	rights, _ := h.auth.GetAPIKeyRights(r.Context(), tenantID, keyID)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	var key *auth.APIKey
	for _, k := range keys {
		if k.ID == keyID {
			key = k
			break
		}
	}
	data := apiKeyRightsPage{basePage: h.base(r, "apikeys"), Tenant: tenant, Key: key, Rights: rights}
	h.renderPartial(w, "apikeys", "apikey-rights-panel", data)
}

func (h *Handler) removeAPIKeyRight(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	keyID := chi.URLParam(r, "keyID")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	_ = h.auth.DeleteRight(r.Context(), tenantID, "apikey", keyID,
		r.FormValue("resource_type"), r.FormValue("resource_id"))

	rights, _ := h.auth.GetAPIKeyRights(r.Context(), tenantID, keyID)
	tenant, _ := h.repo.GetTenant(r.Context(), tenantID)
	keys, _, _ := h.auth.ListAPIKeys(r.Context(), tenantID, 500, 0)
	var key *auth.APIKey
	for _, k := range keys {
		if k.ID == keyID {
			key = k
			break
		}
	}
	data := apiKeyRightsPage{basePage: h.base(r, "apikeys"), Tenant: tenant, Key: key, Rights: rights}
	h.renderPartial(w, "apikeys", "apikey-rights-panel", data)
}

// ── Perm mode handler ─────────────────────────────────────────────────────────

func (h *Handler) setPermMode(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	mode := auth.PermMode(r.FormValue("perm_mode"))
	if mode != auth.PermModeExplicit && mode != auth.PermModeOpen && mode != auth.PermModeInherit {
		http.Error(w, "invalid perm_mode", http.StatusBadRequest)
		return
	}
	if err := h.auth.SetTenantPermMode(r.Context(), tenantID, mode); err != nil {
		http.Error(w, "failed to update permission mode", http.StatusInternalServerError)
		return
	}
	// Return a small success partial that replaces just the select element label.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<span class="badge badge-green">Saved</span>`)
}
```

- [ ] **Step 3: Register the new routes in `web.go`**

Inside the `r.Group(func(r chi.Router)` block in `RegisterRoutes`, add the following routes after the existing API keys block:

```go
			// Rights management — users
			r.Get("/admin/tenants/{tenantID}/users/{userID}/rights", h.userRightsPanel)
			r.Post("/admin/tenants/{tenantID}/users/{userID}/rights", h.addUserRight)
			r.Delete("/admin/tenants/{tenantID}/users/{userID}/rights", h.removeUserRight)

			// Rights management — API keys
			r.Get("/admin/tenants/{tenantID}/apikeys/{keyID}/rights", h.apiKeyRightsPanel)
			r.Post("/admin/tenants/{tenantID}/apikeys/{keyID}/rights", h.addAPIKeyRight)
			r.Delete("/admin/tenants/{tenantID}/apikeys/{keyID}/rights", h.removeAPIKeyRight)

			// Tenant permission mode
			r.Post("/admin/tenants/{tenantID}/settings/permmode", h.setPermMode)
```

- [ ] **Step 4: Add the rights panel template partial to `users.html`**

Append to the end of `internal/web/templates/users.html`:

```html
{{define "user-rights-panel"}}
<div class="card" id="user-rights-panel-{{.User.ID}}">
  <div class="card-header" style="display:flex;align-items:center;justify-content:space-between;padding:12px 16px;border-bottom:1px solid var(--border);">
    <strong>Rights for {{.User.Username}}</strong>
  </div>
  {{if .Rights}}
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>Resource</th>
          <th>Create</th><th>Read</th><th>Update</th><th>Delete</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {{range .Rights}}
        <tr>
          <td><span class="badge badge-gray">{{.ResourceType}}</span> {{.ResourceID}}</td>
          <td>{{if .CanCreate}}✓{{else}}–{{end}}</td>
          <td>{{if .CanRead}}✓{{else}}–{{end}}</td>
          <td>{{if .CanUpdate}}✓{{else}}–{{end}}</td>
          <td>{{if .CanDelete}}✓{{else}}–{{end}}</td>
          <td class="text-right">
            <button class="btn btn-danger btn-sm"
              hx-delete="/admin/tenants/{{$.Tenant.ID}}/users/{{$.User.ID}}/rights"
              hx-vals='{"resource_type":"{{.ResourceType}}","resource_id":"{{.ResourceID}}"}'
              hx-target="#user-rights-panel-{{$.User.ID}}"
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
      <label>Resource type</label>
      <select name="resource_type" id="rt-{{.User.ID}}">
        <option value="project">Project</option>
        <option value="bucket">Bucket</option>
        <option value="document">Document</option>
      </select>
    </div>
    <div class="form-group">
      <label>Resource ID <span class="muted">(or leave blank for all)</span></label>
      <input type="text" name="resource_id" id="rid-{{.User.ID}}" placeholder="* or specific UUID">
    </div>
    <div class="form-group" style="flex-direction:row;gap:8px;align-items:center;">
      <label><input type="checkbox" name="can_create" id="rc-{{.User.ID}}"> Create</label>
      <label><input type="checkbox" name="can_read"   id="rr-{{.User.ID}}"> Read</label>
      <label><input type="checkbox" name="can_update" id="ru-{{.User.ID}}"> Update</label>
      <label><input type="checkbox" name="can_delete" id="rd-{{.User.ID}}"> Delete</label>
    </div>
    <button class="btn btn-primary"
      hx-post="/admin/tenants/{{.Tenant.ID}}/users/{{.User.ID}}/rights"
      hx-include="#rt-{{.User.ID}},#rid-{{.User.ID}},#rc-{{.User.ID}},#rr-{{.User.ID}},#ru-{{.User.ID}},#rd-{{.User.ID}}"
      hx-target="#user-rights-panel-{{.User.ID}}"
      hx-swap="outerHTML">
      Add Right
    </button>
  </div>
</div>
{{end}}
```

- [ ] **Step 5: Add the rights panel partial to `apikeys.html`**

Append to the end of `internal/web/templates/apikeys.html`:

```html
{{define "apikey-rights-panel"}}
<div class="card" id="apikey-rights-panel-{{.Key.ID}}">
  <div class="card-header" style="display:flex;align-items:center;justify-content:space-between;padding:12px 16px;border-bottom:1px solid var(--border);">
    <strong>Rights for {{.Key.Name}}</strong>
  </div>
  {{if .Rights}}
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>Resource</th>
          <th>Create</th><th>Read</th><th>Update</th><th>Delete</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {{range .Rights}}
        <tr>
          <td><span class="badge badge-gray">{{.ResourceType}}</span> {{.ResourceID}}</td>
          <td>{{if .CanCreate}}✓{{else}}–{{end}}</td>
          <td>{{if .CanRead}}✓{{else}}–{{end}}</td>
          <td>{{if .CanUpdate}}✓{{else}}–{{end}}</td>
          <td>{{if .CanDelete}}✓{{else}}–{{end}}</td>
          <td class="text-right">
            <button class="btn btn-danger btn-sm"
              hx-delete="/admin/tenants/{{$.Tenant.ID}}/apikeys/{{$.Key.ID}}/rights"
              hx-vals='{"resource_type":"{{.ResourceType}}","resource_id":"{{.ResourceID}}"}'
              hx-target="#apikey-rights-panel-{{$.Key.ID}}"
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
      <label>Resource type</label>
      <select name="resource_type" id="krt-{{.Key.ID}}">
        <option value="project">Project</option>
        <option value="bucket">Bucket</option>
        <option value="document">Document</option>
      </select>
    </div>
    <div class="form-group">
      <label>Resource ID <span class="muted">(or leave blank for all)</span></label>
      <input type="text" name="resource_id" id="krid-{{.Key.ID}}" placeholder="* or specific UUID">
    </div>
    <div class="form-group" style="flex-direction:row;gap:8px;align-items:center;">
      <label><input type="checkbox" name="can_create" id="krc-{{.Key.ID}}"> Create</label>
      <label><input type="checkbox" name="can_read"   id="krr-{{.Key.ID}}"> Read</label>
      <label><input type="checkbox" name="can_update" id="kru-{{.Key.ID}}"> Update</label>
      <label><input type="checkbox" name="can_delete" id="krd-{{.Key.ID}}"> Delete</label>
    </div>
    <button class="btn btn-primary"
      hx-post="/admin/tenants/{{.Tenant.ID}}/apikeys/{{.Key.ID}}/rights"
      hx-include="#krt-{{.Key.ID}},#krid-{{.Key.ID}},#krc-{{.Key.ID}},#krr-{{.Key.ID}},#kru-{{.Key.ID}},#krd-{{.Key.ID}}"
      hx-target="#apikey-rights-panel-{{.Key.ID}}"
      hx-swap="outerHTML">
      Add Right
    </button>
  </div>
</div>
{{end}}
```

- [ ] **Step 6: Add perm_mode card to `tenants.html`**

Read `tenants.html` first to find the right insertion point (`cat internal/web/templates/tenants.html`), then add a perm_mode card. Find the existing `{{define "tenant-row"}}` block and insert the following **before** it (as a new template partial at the end of the file):

```html
{{define "tenant-perm-mode-card"}}
<div class="card" style="margin-bottom:16px;">
  <div style="padding:14px 16px;border-bottom:1px solid var(--border);">
    <strong>Access policy</strong>
  </div>
  <div style="padding:14px 16px;display:flex;align-items:center;gap:12px;">
    <label style="font-size:13px;font-weight:500;">Default permission mode</label>
    <select name="perm_mode" id="perm-mode-{{.ID}}"
      hx-post="/admin/tenants/{{.ID}}/settings/permmode"
      hx-include="#perm-mode-{{.ID}}"
      hx-target="#perm-mode-save-{{.ID}}"
      hx-swap="innerHTML">
      <option value="explicit" {{if eq .PermMode "explicit"}}selected{{end}}>explicit — grants required</option>
      <option value="open"     {{if eq .PermMode "open"}}selected{{end}}>open — access unless restricted</option>
      <option value="inherit"  {{if eq .PermMode "inherit"}}selected{{end}}>inherit — grants cascade from parent</option>
    </select>
    <span id="perm-mode-save-{{.ID}}"></span>
  </div>
</div>
{{end}}
```

Note: the `tenants.html` `{{define "content"}}` block renders a list of tenants; the perm_mode card belongs on the per-tenant detail/settings view. Since there is no separate per-tenant settings page in the web UI currently, add the card to the **Users** page (already scoped to a tenant) by adding it before the users card:

In `internal/web/templates/users.html`, after the `<div class="page-header">` closing tag and before `<div class="card">`, add:

```html
{{template "tenant-perm-mode-card" .PermModeTenant}}
```

Update the `usersPage` struct in `handlers.go` to include a `PermModeTenant` field with `ID` and `PermMode` strings:

```go
type usersPage struct {
	basePage
	Tenant       *repo.Tenant
	Users        []*auth.User
	Pager        WebPagination
	PermModeTenant struct {
		ID       string
		PermMode string
	}
}
```

And in the `tenantUsers` handler, populate it:

```go
// After loading tenant and users, add:
mode, _ := h.auth.GetTenantPermMode(r.Context(), tenantID)
data.PermModeTenant.ID = tenantID
data.PermModeTenant.PermMode = string(mode)
```

- [ ] **Step 7: Find and update the `usersPage` struct and `tenantUsers` handler**

```bash
grep -n "usersPage\|tenantUsers" internal/web/handlers.go | head -20
```

Read the `usersPage` struct definition and the `tenantUsers` handler. Add the `PermModeTenant` field to the struct, then in the `tenantUsers` handler add the two lines above to populate it before calling `h.render`.

- [ ] **Step 8: Build check**

```bash
go build ./internal/web/...
```

Expected: no errors

- [ ] **Step 9: Run the full test suite**

```bash
go test ./... -v 2>&1 | tail -30
```

Expected: all tests PASS

- [ ] **Step 10: Commit**

```bash
git add internal/web/handlers.go internal/web/web.go \
        internal/web/templates/users.html \
        internal/web/templates/apikeys.html \
        internal/web/templates/tenants.html
git commit -m "feat(web): add rights panel on user/API key pages and perm_mode selector on tenant settings"
```

---

## Task 6: Push to main

- [ ] **Step 1: Run the complete test suite one final time**

```bash
go test ./... -v 2>&1 | grep -E "^(ok|FAIL|---)" | head -40
```

Expected: all packages show `ok`

- [ ] **Step 2: Push**

```bash
git push origin main
```
