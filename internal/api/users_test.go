package api_test

import (
	"net/http"
	"testing"

	"tinydm/internal/auth"
)

// TestUsers_List_IncludesFirstAndLastName verifies that the safeUser JSON
// returned by GET /api/v1/users now exposes first_name and last_name.
func TestUsers_List_IncludesFirstAndLastName(t *testing.T) {
	ts := newTestServer(t)
	ts.seedAdminUser(t, "alice", "secret")
	token := ts.login(t, "alice", "secret")

	var result struct {
		Data []map[string]any `json:"data"`
	}
	resp := ts.doJSON(t, http.MethodGet,
		"/api/v1/users", nil, bearer(token), &result)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	if len(result.Data) == 0 {
		t.Fatalf("expected at least one user, got 0")
	}
	if _, ok := result.Data[0]["first_name"]; !ok {
		t.Errorf("expected first_name in user payload, got %v", result.Data[0])
	}
	if _, ok := result.Data[0]["last_name"]; !ok {
		t.Errorf("expected last_name in user payload, got %v", result.Data[0])
	}
}

// TestChangePassword_Success: an admin can change a user's password and the
// new password authenticates while the old one no longer does.
func TestChangePassword_Success(t *testing.T) {
	ts := newTestServer(t)
	admin := ts.seedAdminUser(t, "alice", "admin-pass")
	token := ts.login(t, "alice", "admin-pass")

	resp := ts.doJSON(t, http.MethodPatch,
		"/api/v1/users/"+admin.ID+"/password",
		map[string]string{"password": "brandnewpass"},
		bearer(token), nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusNoContent)

	// Old password no longer works.
	bad := ts.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "alice",
		"password": "admin-pass",
	}, nil, nil)
	bad.Body.Close()
	if bad.StatusCode == http.StatusOK {
		t.Errorf("old password still authenticates after change")
	}

	// New password works.
	good := ts.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "alice",
		"password": "brandnewpass",
	}, nil, nil)
	good.Body.Close()
	if good.StatusCode != http.StatusOK {
		t.Errorf("new password failed to authenticate: status %d", good.StatusCode)
	}
}

// TestChangePassword_TooShort returns 400 when the password is shorter than 8.
func TestChangePassword_TooShort(t *testing.T) {
	ts := newTestServer(t)
	admin := ts.seedAdminUser(t, "alice", "admin-pass")
	token := ts.login(t, "alice", "admin-pass")

	resp := ts.doJSON(t, http.MethodPatch,
		"/api/v1/users/"+admin.ID+"/password",
		map[string]string{"password": "short"},
		bearer(token), nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusBadRequest)
}

// TestChangePassword_RequiresAdmin returns 403 when caller is a plain user.
func TestChangePassword_RequiresAdmin(t *testing.T) {
	ts := newTestServer(t)
	admin := ts.seedAdminUser(t, "alice", "admin-pass")
	_ = admin

	// Seed a regular user.
	regular := ts.seedUserWithType(t, "bob", "userpass", auth.UserTypeUser)
	bobToken := ts.login(t, "bob", "userpass")

	// Bob (a 'user' role) tries to change someone else's password — must 403.
	resp := ts.doJSON(t, http.MethodPatch,
		"/api/v1/users/"+regular.ID+"/password",
		map[string]string{"password": "newpassword"},
		bearer(bobToken), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("got status %d, want 403", resp.StatusCode)
	}
}
