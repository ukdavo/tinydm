package auth

import (
	"context"
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

// seedTenant inserts a bare tenant row so FK constraints on users are satisfied.
func seedTenant(t *testing.T, s *Store, id string) {
	t.Helper()
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO tenants (id, name, description) VALUES (?, ?, ?)`, id, id, "")
	if err != nil {
		t.Fatalf("seedTenant %q: %v", id, err)
	}
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
