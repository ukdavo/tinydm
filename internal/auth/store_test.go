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
	superadmin, err := s.CreateUser(ctx, "tenant-sys", "superadmin", "", hash, UserTypeSuperAdmin)
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
	user, err := s.CreateUser(ctx, "tenant-1", "alice", "", hash, UserTypeUser)
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
	superadmin, err := s.CreateUser(ctx, "tenant-sys", "superadmin", "", hash, UserTypeSuperAdmin)
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
	admin, err := s.CreateUser(ctx, "tenant-1", "admin", "", hash, UserTypeAdmin)
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
	user, err := s.CreateUser(ctx, "tenant-1", "alice", "", hash, UserTypeUser)
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
	user, err := s.CreateUser(ctx, "tenant-1", "alice", "", hash, UserTypeUser)
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
