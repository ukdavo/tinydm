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

// randomSuffix returns a short random hex string for unique IDs in tests.
func randomSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", b)
}

// seedUser inserts a user row and returns the created User.
func seedUser(t *testing.T, s *Store, username string, userType UserType) *User {
	t.Helper()
	hash, err := HashPassword("testpass")
	if err != nil {
		t.Fatalf("seedUser HashPassword: %v", err)
	}
	u, err := s.CreateUser(context.Background(), username, "", username, username, hash, userType)
	if err != nil {
		t.Fatalf("seedUser CreateUser %q: %v", username, err)
	}
	return u
}

func TestDeleteUser_RejectsSuperadmin(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	hash, _ := HashPassword("secret")
	admin, err := s.CreateUser(ctx, "admin", "", "Admin", "User", hash, UserTypeAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := s.DeleteUser(ctx, admin.ID); err == nil {
		t.Error("expected error deleting admin, got nil")
	}
}

func TestDeleteUser_AllowsRegularUser(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	hash, _ := HashPassword("secret")
	user, err := s.CreateUser(ctx, "alice", "", "Alice", "Smith", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := s.DeleteUser(ctx, user.ID); err != nil {
		t.Errorf("unexpected error deleting regular user: %v", err)
	}
}

func TestSetUserActive_RejectsAdminDeactivation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	hash, _ := HashPassword("secret")
	admin, err := s.CreateUser(ctx, "admin", "", "Admin", "User", hash, UserTypeAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := s.SetUserActive(ctx, admin.ID, false); err == nil {
		t.Error("expected error deactivating admin, got nil")
	}
}

func TestSetUserActive_AllowsRegularUserDeactivation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	hash, _ := HashPassword("secret")
	user, err := s.CreateUser(ctx, "alice", "", "Alice", "Smith", hash, UserTypeUser)
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

	hash, _ := HashPassword("secret")
	user, err := s.CreateUser(ctx, "alice", "", "Alice", "Smith", hash, UserTypeUser)
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

	hash, _ := HashPassword("secret")
	user, err := s.CreateUser(ctx, "alice", "alice@example.com", "Alice", "Smith", hash, UserTypeUser)
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

	hash, _ := HashPassword("secret")
	if _, err := s.CreateUser(ctx, "alice", "", "Alice", "Smith", hash, UserTypeUser); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	users, _, err := s.ListUsers(ctx, 0, 0)
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

	oldHash, _ := HashPassword("oldpass")
	user, err := s.CreateUser(ctx, "alice", "", "Alice", "Smith", oldHash, UserTypeUser)
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

func TestCountUsers_ReturnsCorrectCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	hash, _ := HashPassword("pass")
	_, err := s.CreateUser(ctx, "alice", "alice@test.local", "Alice", "A", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, err = s.CreateUser(ctx, "bob", "bob@test.local", "Bob", "B", hash, UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	n, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

func TestCountAPIKeys_ReturnsCorrectCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	hash, _ := HashPassword("pass")
	u1, _ := s.CreateUser(ctx, "alice", "alice@t.local", "Alice", "A", hash, UserTypeAdmin)
	u2, _ := s.CreateUser(ctx, "bob", "bob@t.local", "Bob", "B", hash, UserTypeAdmin)

	_, keyHash1, keyPrefix1, _ := GenerateAPIKey()
	_, err := s.CreateAPIKey(ctx, &u1.ID, "key1", keyHash1, keyPrefix1, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	_, keyHash2, keyPrefix2, _ := GenerateAPIKey()
	_, err = s.CreateAPIKey(ctx, &u1.ID, "key2", keyHash2, keyPrefix2, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	_, keyHash3, keyPrefix3, _ := GenerateAPIKey()
	_, err = s.CreateAPIKey(ctx, &u2.ID, "other", keyHash3, keyPrefix3, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	// Verify total key count.
	keys, total, err := s.ListAPIKeys(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if total != 3 {
		t.Errorf("got total=%d, want 3", total)
	}
	_ = keys
}

// ─── Rights store methods ─────────────────────────────────────────────────────

func TestUpsertRight_CreateAndUpdate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	user := seedUser(t, s, "alice", UserTypeUser)

	params := UpsertRightParams{
		PrincipalType: "user",
		PrincipalID:   user.ID,
		ResourceType:  "project",
		ResourceID:    "proj-1",
		CanRead:       true,
	}
	if err := s.UpsertRight(ctx, params); err != nil {
		t.Fatalf("UpsertRight: %v", err)
	}

	rights, err := s.ListRights(ctx, "user", user.ID)
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
	rights2, _ := s.ListRights(ctx, "user", user.ID)
	if !rights2[0].CanDelete {
		t.Error("CanDelete should be true after update")
	}
}

func TestDeleteRight_RemovesRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	user := seedUser(t, s, "bob", UserTypeUser)

	params := UpsertRightParams{
		PrincipalType: "user",
		PrincipalID:   user.ID,
		ResourceType:  "bucket",
		ResourceID:    "bucket-1",
		CanRead:       true,
	}
	_ = s.UpsertRight(ctx, params)

	if err := s.DeleteRight(ctx, "user", user.ID, "bucket", "bucket-1"); err != nil {
		t.Fatalf("DeleteRight: %v", err)
	}

	rights, _ := s.ListRights(ctx, "user", user.ID)
	if len(rights) != 0 {
		t.Errorf("expected 0 rights after delete, got %d", len(rights))
	}
}

func TestDeleteRight_NotFound_ReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.DeleteRight(ctx, "user", "no-user", "project", "no-proj")
	if err == nil {
		t.Error("expected error deleting non-existent right")
	}
}

func TestListRights_IsolatesByPrincipal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	alice := seedUser(t, s, "alice", UserTypeUser)
	bob := seedUser(t, s, "bob", UserTypeUser)

	_ = s.UpsertRight(ctx, UpsertRightParams{
		PrincipalType: "user", PrincipalID: alice.ID,
		ResourceType: "project", ResourceID: "proj-1", CanRead: true,
	})
	_ = s.UpsertRight(ctx, UpsertRightParams{
		PrincipalType: "user", PrincipalID: bob.ID,
		ResourceType: "project", ResourceID: "proj-2", CanRead: true,
	})

	aliceRights, _ := s.ListRights(ctx, "user", alice.ID)
	if len(aliceRights) != 1 || aliceRights[0].ResourceID != "proj-1" {
		t.Errorf("alice should have exactly 1 right on proj-1, got %+v", aliceRights)
	}

	bobRights, _ := s.ListRights(ctx, "user", bob.ID)
	if len(bobRights) != 1 || bobRights[0].ResourceID != "proj-2" {
		t.Errorf("bob should have exactly 1 right on proj-2, got %+v", bobRights)
	}
}

func TestGetAPIKeyRights_ReturnOnlyAPIKeyRights(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	user := seedUser(t, s, "alice", UserTypeUser)

	// Grant a right to the user (principal_type=user).
	_ = s.UpsertRight(ctx, UpsertRightParams{
		PrincipalType: "user", PrincipalID: user.ID,
		ResourceType: "project", ResourceID: "proj-1", CanRead: true,
	})

	// Grant a right to an API key (principal_type=apikey).
	keyID := "apikey-test-id"
	_ = s.UpsertRight(ctx, UpsertRightParams{
		PrincipalType: "apikey", PrincipalID: keyID,
		ResourceType: "bucket", ResourceID: "bucket-1", CanRead: true,
	})

	rights, err := s.GetAPIKeyRights(ctx, keyID)
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

// ─── GetAPIKeyByID tests ──────────────────────────────────────────────────────

func TestGetAPIKeyByID_Found(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	user := seedUser(t, s, "alice", UserTypeUser)

	_, keyHash, keyPrefix, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	created, err := s.CreateAPIKey(ctx, &user.ID, "my-key", keyHash, keyPrefix, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	got, err := s.GetAPIKeyByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil APIKey, got nil")
	}
	if got.Name != "my-key" {
		t.Errorf("Name: got %q, want %q", got.Name, "my-key")
	}
	if got.KeyPrefix != keyPrefix {
		t.Errorf("KeyPrefix: got %q, want %q", got.KeyPrefix, keyPrefix)
	}
}

func TestGetAPIKeyByID_Missing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetAPIKeyByID(ctx, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("GetAPIKeyByID: unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing key, got %+v", got)
	}
}

// ─── ListRightsByResource tests ───────────────────────────────────────────────

func TestListRightsByResource(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	alice := seedUser(t, s, "alice", UserTypeUser)
	bob := seedUser(t, s, "bob", UserTypeUser)

	// Two rights on the same project resource.
	_ = s.UpsertRight(ctx, UpsertRightParams{
		PrincipalType: "user", PrincipalID: alice.ID,
		ResourceType: "project", ResourceID: "proj-1", CanRead: true,
	})
	_ = s.UpsertRight(ctx, UpsertRightParams{
		PrincipalType: "user", PrincipalID: bob.ID,
		ResourceType: "project", ResourceID: "proj-1", CanRead: true, CanUpdate: true,
	})

	// One right on a different resource (should be excluded).
	_ = s.UpsertRight(ctx, UpsertRightParams{
		PrincipalType: "user", PrincipalID: alice.ID,
		ResourceType: "bucket", ResourceID: "bucket-1", CanRead: true,
	})

	rights, err := s.ListRightsByResource(ctx, "project", "proj-1")
	if err != nil {
		t.Fatalf("ListRightsByResource: %v", err)
	}
	if len(rights) != 2 {
		t.Fatalf("expected 2 rights for proj-1, got %d: %+v", len(rights), rights)
	}
	for _, r := range rights {
		if r.ResourceID == "bucket-1" {
			t.Errorf("bucket-1 right should not appear in project results")
		}
		if r.ResourceType != "project" {
			t.Errorf("expected resource_type=project, got %q", r.ResourceType)
		}
	}
}
