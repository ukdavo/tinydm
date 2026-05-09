package api_test

import (
	"net/http"
	"testing"
)

// ─── Login ────────────────────────────────────────────────────────────────────

func TestLogin_Success(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedAdminUser(t, "Acme", "alice", "secret123")

	var result map[string]any
	resp := ts.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"tenant_id": tenant.ID,
		"username":  "alice",
		"password":  "secret123",
	}, nil, &result)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusOK)

	if result["token"] == nil || result["token"] == "" {
		t.Error("expected non-empty token in response")
	}
	if result["username"] != "alice" {
		t.Errorf("username: got %v, want alice", result["username"])
	}
	if result["tenant_id"] != tenant.ID {
		t.Errorf("tenant_id: got %v, want %s", result["tenant_id"], tenant.ID)
	}
	if result["user_type"] != "admin" {
		t.Errorf("user_type: got %v, want admin", result["user_type"])
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedAdminUser(t, "Acme", "alice", "correct-password")

	resp := ts.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"tenant_id": tenant.ID,
		"username":  "alice",
		"password":  "wrong-password",
	}, nil, nil)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestLogin_UnknownUser(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedAdminUser(t, "Acme", "alice", "secret")

	resp := ts.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"tenant_id": tenant.ID,
		"username":  "nobody",
		"password":  "secret",
	}, nil, nil)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestLogin_MissingFields(t *testing.T) {
	ts := newTestServer(t)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"missing tenant_id", map[string]string{"username": "alice", "password": "x"}},
		{"missing username", map[string]string{"tenant_id": "t1", "password": "x"}},
		{"missing password", map[string]string{"tenant_id": "t1", "username": "alice"}},
		{"empty body", map[string]string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := ts.doJSON(t, http.MethodPost, "/api/v1/auth/login", tc.body, nil, nil)
			defer resp.Body.Close()
			assertStatus(t, resp, http.StatusBadRequest)
		})
	}
}

// ─── /auth/me ─────────────────────────────────────────────────────────────────

func TestMe_Authenticated(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedAdminUser(t, "Acme", "alice", "secret123")
	token := ts.login(t, tenant.ID, "alice", "secret123")

	var result map[string]any
	resp := ts.doJSON(t, http.MethodGet, "/api/v1/auth/me", nil, bearer(token), &result)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusOK)
	if result["username"] != "alice" {
		t.Errorf("username: got %v, want alice", result["username"])
	}
	if result["user_type"] != "admin" {
		t.Errorf("user_type: got %v, want admin", result["user_type"])
	}
	if result["auth_method"] != "bearer" {
		t.Errorf("auth_method: got %v, want bearer", result["auth_method"])
	}
}

func TestMe_Unauthenticated(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.do(t, http.MethodGet, "/api/v1/auth/me", nil, nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestMe_InvalidToken(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.do(t, http.MethodGet, "/api/v1/auth/me", nil, map[string]string{
		"Authorization": "Bearer not-a-valid-jwt",
	})
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusUnauthorized)
}

// ─── Protected routes reject unauthenticated requests ─────────────────────────

func TestProtectedRoutes_RequireAuth(t *testing.T) {
	ts := newTestServer(t)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/tenants"},
		{http.MethodGet, "/api/v1/auth/me"},
	}

	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			resp := ts.do(t, r.method, r.path, nil, nil)
			defer resp.Body.Close()
			assertStatus(t, resp, http.StatusUnauthorized)
		})
	}
}

// ─── Health check ─────────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	var result map[string]any
	resp := ts.doJSON(t, http.MethodGet, "/health", nil, nil, &result)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusOK)
	if result["status"] != "ok" {
		t.Errorf("status: got %v, want ok", result["status"])
	}
}
