package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"path/filepath"
	"testing"

	"tinydm/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	sqlDB, err := db.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return NewStore(sqlDB)
}

// testTenant is a minimal tenant record returned by seedTenant.
type testTenant struct {
	ID string
}

// seedTenant inserts a bare tenant row so FK constraints on users are satisfied.
// If an id argument is provided it is used as-is; otherwise a UUID is generated.
func seedTenant(t *testing.T, s *Store, ids ...string) testTenant {
	t.Helper()
	id := ""
	if len(ids) > 0 {
		id = ids[0]
	}
	if id == "" {
		id = "tenant-" + randomSuffix()
	}
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO tenants (id, name, description) VALUES (?, ?, ?)`, id, id, "")
	if err != nil {
		t.Fatalf("seedTenant %q: %v", id, err)
	}
	return testTenant{ID: id}
}

// randomSuffix returns a short random hex string for unique IDs in tests.
func randomSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", b)
}

// seedUser inserts a user row for the given tenant and returns the created User.
func seedUser(t *testing.T, s *Store, tenantID, username string, userType UserType) *User {
	t.Helper()
	hash, err := HashPassword("testpass")
	if err != nil {
		t.Fatalf("seedUser HashPassword: %v", err)
	}
	u, err := s.CreateUser(context.Background(), tenantID, username, "", username, username, hash, userType)
	if err != nil {
		t.Fatalf("seedUser CreateUser %q: %v", username, err)
	}
	return u
}

func TestCreateDomainAdmin_CreatesAdminUser(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")

	user, plaintext, err := s.CreateDomainAdmin(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("CreateDomainAdmin: %v", err)
	}

	if user.UserType != UserTypeAdmin {
		t.Errorf("user_type: got %q, want %q", user.UserType, UserTypeAdmin)
	}
	if user.TenantID != "tenant-1" {
		t.Errorf("tenant_id: got %q, want %q", user.TenantID, "tenant-1")
	}
	if plaintext == "" {
		t.Error("expected non-empty plaintext password")
	}
}

func TestCreateDomainAdmin_PasswordAuthenticates(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")

	user, plaintext, err := s.CreateDomainAdmin(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("CreateDomainAdmin: %v", err)
	}

	// The stored hash must match the returned plaintext.
	if err := CheckPassword(user.PasswordHash, plaintext); err != nil {
		t.Errorf("returned plaintext does not match stored hash: %v", err)
	}
}

func TestCreateDomainAdmin_PlaintextNotStoredInHash(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")

	user, plaintext, err := s.CreateDomainAdmin(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("CreateDomainAdmin: %v", err)
	}

	if user.PasswordHash == plaintext {
		t.Error("password_hash must not equal the plaintext password")
	}
}

func TestDeleteUser_RejectsSuperadmin(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-sys")

	hash, _ := HashPassword("secret")
	superadmin, err := s.CreateUser(ctx, "tenant-sys", "superadmin", "", "Super", "Admin", hash, UserTypeSuperAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := s.DeleteUser(ctx, superadmin.ID); err == nil {
		t.Error("expected error deleting superadmin, got nil")
	}
}

func TestDeleteUser_AllowsRegularUser(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")

	hash, _ := HashPassword("secret")
	user, err := s.CreateUser(ctx, "tenant-1", "alice", "", "Alice", "Smith", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := s.DeleteUser(ctx, user.ID); err != nil {
		t.Errorf("unexpected error deleting regular user: %v", err)
	}
}

func TestSetUserActive_RejectsSuperadminDeactivation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-sys")

	hash, _ := HashPassword("secret")
	superadmin, err := s.CreateUser(ctx, "tenant-sys", "superadmin", "", "Super", "Admin", hash, UserTypeSuperAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := s.SetUserActive(ctx, superadmin.ID, false); err == nil {
		t.Error("expected error deactivating superadmin, got nil")
	}
}

func TestSetUserActive_RejectsDomainAdminDeactivation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")

	hash, _ := HashPassword("secret")
	admin, err := s.CreateUser(ctx, "tenant-1", "admin", "", "Domain", "Admin", hash, UserTypeAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := s.SetUserActive(ctx, admin.ID, false); err == nil {
		t.Error("expected error deactivating domain admin, got nil")
	}
}

func TestSetUserActive_AllowsRegularUserDeactivation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")

	hash, _ := HashPassword("secret")
	user, err := s.CreateUser(ctx, "tenant-1", "alice", "", "Alice", "Smith", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := s.SetUserActive(ctx, user.ID, false); err != nil {
		t.Errorf("unexpected error deactivating regular user: %v", err)
	}
}

func TestSetUserActive_AllowsReactivation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")

	hash, _ := HashPassword("secret")
	user, err := s.CreateUser(ctx, "tenant-1", "alice", "", "Alice", "Smith", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.SetUserActive(ctx, user.ID, false); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if err := s.SetUserActive(ctx, user.ID, true); err != nil {
		t.Errorf("unexpected error reactivating user: %v", err)
	}
}

func TestCreateUser_PersistsFirstAndLastName(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")

	hash, _ := HashPassword("secret")
	user, err := s.CreateUser(ctx, "tenant-1", "alice", "alice@example.com", "Alice", "Smith", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.FirstName != "Alice" {
		t.Errorf("first_name: got %q, want %q", user.FirstName, "Alice")
	}
	if user.LastName != "Smith" {
		t.Errorf("last_name: got %q, want %q", user.LastName, "Smith")
	}

	// Round-trip via GetUserByID.
	again, err := s.GetUserByID(ctx, user.ID)
	if err != nil || again == nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if again.FirstName != "Alice" || again.LastName != "Smith" {
		t.Errorf("round-trip: got %q %q, want Alice Smith", again.FirstName, again.LastName)
	}
}

func TestListUsers_ReturnsFirstAndLastName(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")

	hash, _ := HashPassword("secret")
	if _, err := s.CreateUser(ctx, "tenant-1", "alice", "", "Alice", "Smith", hash, UserTypeUser); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	users, _, err := s.ListUsers(ctx, "tenant-1", 0, 0)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("got %d users, want 1", len(users))
	}
	if users[0].FirstName != "Alice" || users[0].LastName != "Smith" {
		t.Errorf("ListUsers names: got %q %q, want Alice Smith", users[0].FirstName, users[0].LastName)
	}
}

func TestChangePassword_UpdatesHash(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")

	oldHash, _ := HashPassword("oldpass")
	user, err := s.CreateUser(ctx, "tenant-1", "alice", "", "Alice", "Smith", oldHash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	newHash, _ := HashPassword("newpass")
	if err := s.ChangePassword(ctx, user.ID, newHash); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	updated, err := s.GetUserByID(ctx, user.ID)
	if err != nil || updated == nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if err := CheckPassword(updated.PasswordHash, "newpass"); err != nil {
		t.Errorf("new password does not authenticate: %v", err)
	}
	if err := CheckPassword(updated.PasswordHash, "oldpass"); err == nil {
		t.Error("old password still authenticates after change")
	}
}

func TestChangePassword_UnknownUserReturnsError(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	hash, _ := HashPassword("anything")
	if err := s.ChangePassword(ctx, "no-such-user", hash); err == nil {
		t.Error("expected error for unknown user, got nil")
	}
}

func TestCountUsersByTenant_ReturnsCorrectCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")
	seedTenant(t, s, "tenant-2")

	hash, _ := HashPassword("pass")
	_, err := s.CreateUser(ctx, "tenant-1", "alice", "alice@test.local", "Alice", "A", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, err = s.CreateUser(ctx, "tenant-1", "bob", "bob@test.local", "Bob", "B", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, err = s.CreateUser(ctx, "tenant-2", "carol", "carol@test.local", "Carol", "C", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	n, err := s.CountUsersByTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("CountUsersByTenant: %v", err)
	}
	if n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

func TestCountAPIKeysByTenant_ReturnsCorrectCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedTenant(t, s, "tenant-1")
	seedTenant(t, s, "tenant-2")

	hash, _ := HashPassword("pass")
	u1, _ := s.CreateUser(ctx, "tenant-1", "alice", "alice@t.local", "Alice", "A", hash, UserTypeAdmin)
	u2, _ := s.CreateUser(ctx, "tenant-2", "bob", "bob@t.local", "Bob", "B", hash, UserTypeAdmin)

	_, keyHash1, keyPrefix1, _ := GenerateAPIKey()
	_, err := s.CreateAPIKey(ctx, u1.TenantID, &u1.ID, "key1", keyHash1, keyPrefix1, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	_, keyHash2, keyPrefix2, _ := GenerateAPIKey()
	_, err = s.CreateAPIKey(ctx, u1.TenantID, &u1.ID, "key2", keyHash2, keyPrefix2, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	_, keyHash3, keyPrefix3, _ := GenerateAPIKey()
	_, err = s.CreateAPIKey(ctx, u2.TenantID, &u2.ID, "other", keyHash3, keyPrefix3, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	n, err := s.CountAPIKeysByTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("CountAPIKeysByTenant: %v", err)
	}
	if n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

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
