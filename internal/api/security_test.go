package api_test

import (
	"context"
	"net/http"
	"testing"

	"tinydm/internal/auth"
)

// seedRegularUser creates a regular (non-admin) user and returns a Bearer token for that user.
func (ts *testServer) seedRegularUser(t *testing.T, username, password string) string {
	t.Helper()
	ctx := context.Background()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	_, err = ts.authStore.CreateUser(ctx, username, username+"@test.local", "Test", "User", hash, auth.UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return ts.login(t, username, password)
}

func TestPermissions_RegularUser_DeniedWithoutGrant(t *testing.T) {
	ts := newTestServer(t)
	ts.seedAdminUser(t, "admin", "pass")
	adminToken := ts.login(t, "admin", "pass")
	userToken := ts.seedRegularUser(t, "alice", "pass")

	// Admin creates a project.
	var proj map[string]any
	ts.doJSON(t, http.MethodPost,
		"/api/v1/projects",
		map[string]string{"name": "Alpha"}, bearer(adminToken), &proj)
	projID := proj["id"].(string)

	// Regular user must be denied listing projects (no right granted).
	resp := ts.doJSON(t, http.MethodGet,
		"/api/v1/projects",
		nil, bearer(userToken), nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusForbidden)

	// Regular user must be denied creating a project.
	resp2 := ts.doJSON(t, http.MethodPost,
		"/api/v1/projects",
		map[string]string{"name": "Sneaky"}, bearer(userToken), nil)
	defer resp2.Body.Close()
	assertStatus(t, resp2, http.StatusForbidden)

	_ = projID
}

func TestPermissions_RegularUser_AllowedAfterGrant(t *testing.T) {
	ts := newTestServer(t)
	user := ts.seedAdminUser(t, "admin", "pass")
	adminToken := ts.login(t, "admin", "pass")
	alice, _ := ts.authStore.CreateUser(context.Background(), "alice", "alice@test.local", "Alice", "Smith", mustHash(t, "pass"), auth.UserTypeUser)
	aliceToken := ts.login(t, "alice", "pass")

	// Admin creates a project.
	var proj map[string]any
	ts.doJSON(t, http.MethodPost,
		"/api/v1/projects",
		map[string]string{"name": "Alpha"}, bearer(adminToken), &proj)
	projID := proj["id"].(string)

	// Grant alice read on projects (wildcard).
	err := ts.authStore.UpsertRight(context.Background(), auth.UpsertRightParams{
		PrincipalType: "user", PrincipalID: alice.ID,
		ResourceType: "project", ResourceID: "*", CanRead: true,
	})
	if err != nil {
		t.Fatalf("UpsertRight: %v", err)
	}

	// Alice should now be able to list projects.
	var list map[string]any
	resp := ts.doJSON(t, http.MethodGet,
		"/api/v1/projects",
		nil, bearer(aliceToken), &list)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	_ = user
	_ = projID
}

func TestPermissions_AdminBypassesRights(t *testing.T) {
	ts := newTestServer(t)
	ts.seedAdminUser(t, "admin", "pass")
	adminToken := ts.login(t, "admin", "pass")

	// Admin can list projects with no rights configured.
	resp := ts.doJSON(t, http.MethodGet,
		"/api/v1/projects",
		nil, bearer(adminToken), nil)
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
