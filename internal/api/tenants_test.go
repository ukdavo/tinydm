package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"tinydm/internal/auth"
)

// ─── Tenant CRUD ──────────────────────────────────────────────────────────────

func TestTenants_List(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedSuperadminUser(t, "Acme", "superadmin", "password")
	token := ts.login(t, tenant.ID, "superadmin", "password")

	// Create a second tenant to ensure list returns multiple results.
	ts.doJSON(t, http.MethodPost, "/api/v1/tenants", map[string]string{
		"name": "Beta Corp",
	}, bearer(token), nil)

	var result map[string]any
	resp := ts.doJSON(t, http.MethodGet, "/api/v1/tenants", nil, bearer(token), &result)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusOK)

	// Envelope must have "data" and "pagination" keys.
	if result["data"] == nil {
		t.Error("expected 'data' key in response")
	}
	if result["pagination"] == nil {
		t.Error("expected 'pagination' key in response")
	}

	data, ok := result["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array: %T", result["data"])
	}
	if len(data) < 1 {
		t.Error("expected at least one tenant in list")
	}
}

func TestTenants_Create(t *testing.T) {
	ts := newTestServer(t)
	// Tenant create requires superadmin.
	tenant, _ := ts.seedSuperadminUser(t, "System", "superadmin", "password")
	token := ts.login(t, tenant.ID, "superadmin", "password")

	var created map[string]any
	resp := ts.doJSON(t, http.MethodPost, "/api/v1/tenants", map[string]string{
		"name":        "New Tenant",
		"description": "Created in test",
	}, bearer(token), &created)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusCreated)
	if created["id"] == nil {
		t.Error("expected id in created tenant")
	}
	if created["name"] != "New Tenant" {
		t.Errorf("name: got %v, want 'New Tenant'", created["name"])
	}
	// Response must include one-time domain admin credentials.
	if created["admin_username"] == nil {
		t.Error("expected admin_username in create-tenant response")
	}
	if created["admin_password"] == nil {
		t.Error("expected admin_password in create-tenant response")
	}
}

func TestTenants_Create_RequiresSuperAdmin(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedAdminUser(t, "Acme", "admin", "password")

	// A plain domain admin must be rejected (403).
	adminToken, err := auth.NewJWT(testJWTSecret, 60,
		"admin-id-1", tenant.ID, "admin", auth.UserTypeAdmin)
	if err != nil {
		t.Fatalf("NewJWT: %v", err)
	}
	resp := ts.doJSON(t, http.MethodPost, "/api/v1/tenants", map[string]string{
		"name": "Sneaky Tenant",
	}, bearer(adminToken), nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusForbidden)

	// A regular user must also be rejected (403).
	userToken, err := auth.NewJWT(testJWTSecret, 60,
		"user-id-1", tenant.ID, "regular", auth.UserTypeUser)
	if err != nil {
		t.Fatalf("NewJWT: %v", err)
	}
	resp2 := ts.doJSON(t, http.MethodPost, "/api/v1/tenants", map[string]string{
		"name": "Sneaky Tenant",
	}, bearer(userToken), nil)
	defer resp2.Body.Close()
	assertStatus(t, resp2, http.StatusForbidden)
}

func TestTenants_Get(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedAdminUser(t, "Globex", "admin", "pass")
	token := ts.login(t, tenant.ID, "admin", "pass")

	var result map[string]any
	resp := ts.doJSON(t, http.MethodGet,
		fmt.Sprintf("/api/v1/tenants/%s", tenant.ID),
		nil, bearer(token), &result)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusOK)
	if result["id"] != tenant.ID {
		t.Errorf("id: got %v, want %s", result["id"], tenant.ID)
	}
	if result["name"] != "Globex" {
		t.Errorf("name: got %v, want Globex", result["name"])
	}
}

func TestTenants_Get_NotFound(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedAdminUser(t, "Acme", "admin", "pass")
	token := ts.login(t, tenant.ID, "admin", "pass")

	resp := ts.do(t, http.MethodGet, "/api/v1/tenants/does-not-exist", nil, bearer(token))
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusNotFound)
}

func TestTenants_Update(t *testing.T) {
	ts := newTestServer(t)
	// Tenant update requires superadmin.
	tenant, _ := ts.seedSuperadminUser(t, "Old Name", "superadmin", "pass")
	token := ts.login(t, tenant.ID, "superadmin", "pass")

	var updated map[string]any
	resp := ts.doJSON(t, http.MethodPut,
		fmt.Sprintf("/api/v1/tenants/%s", tenant.ID),
		map[string]string{"name": "New Name", "description": "updated"},
		bearer(token), &updated)
	defer resp.Body.Close()

	assertStatus(t, resp, http.StatusOK)
	if updated["name"] != "New Name" {
		t.Errorf("name after update: got %v, want 'New Name'", updated["name"])
	}
}

func TestTenants_Delete(t *testing.T) {
	ts := newTestServer(t)
	// Tenant delete requires superadmin.
	tenant, _ := ts.seedSuperadminUser(t, "DeleteMe", "superadmin", "pass")
	token := ts.login(t, tenant.ID, "superadmin", "pass")

	// Superadmin deletes the tenant.
	resp := ts.do(t, http.MethodDelete,
		fmt.Sprintf("/api/v1/tenants/%s", tenant.ID),
		nil, bearer(token))
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusNoContent)

	// Confirm it is gone.
	resp2 := ts.do(t, http.MethodGet,
		fmt.Sprintf("/api/v1/tenants/%s", tenant.ID),
		nil, bearer(token))
	defer resp2.Body.Close()
	assertStatus(t, resp2, http.StatusNotFound)
}

func TestTenants_Delete_CrossTenantBlocked(t *testing.T) {
	ts := newTestServer(t)
	tenantA, _ := ts.seedAdminUser(t, "Tenant A", "adminA", "pass")
	tenantB, _ := ts.seedAdminUser(t, "Tenant B", "adminB", "pass")
	tokenA := ts.login(t, tenantA.ID, "adminA", "pass")

	// Admin of tenant A must not be able to delete tenant B.
	resp := ts.do(t, http.MethodDelete,
		fmt.Sprintf("/api/v1/tenants/%s", tenantB.ID),
		nil, bearer(tokenA))
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusForbidden)
}

// ─── Project CRUD ─────────────────────────────────────────────────────────────

func TestProjects_CreateAndList(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedAdminUser(t, "Acme", "admin", "pass")
	token := ts.login(t, tenant.ID, "admin", "pass")

	basePath := fmt.Sprintf("/api/v1/tenants/%s/projects", tenant.ID)

	// Create.
	var proj map[string]any
	resp := ts.doJSON(t, http.MethodPost, basePath,
		map[string]string{"name": "Alpha", "description": "first project"},
		bearer(token), &proj)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusCreated)

	if proj["name"] != "Alpha" {
		t.Errorf("name: got %v, want Alpha", proj["name"])
	}

	// List.
	var list map[string]any
	resp2 := ts.doJSON(t, http.MethodGet, basePath, nil, bearer(token), &list)
	defer resp2.Body.Close()
	assertStatus(t, resp2, http.StatusOK)

	data := list["data"].([]any)
	if len(data) == 0 {
		t.Error("expected at least one project in list")
	}
}

// ─── Bucket CRUD ──────────────────────────────────────────────────────────────

func TestBuckets_CreateAndList(t *testing.T) {
	ts := newTestServer(t)
	tenant, _ := ts.seedAdminUser(t, "Acme", "admin", "pass")
	token := ts.login(t, tenant.ID, "admin", "pass")

	// Create project.
	var proj map[string]any
	ts.doJSON(t, http.MethodPost,
		fmt.Sprintf("/api/v1/tenants/%s/projects", tenant.ID),
		map[string]string{"name": "Proj1"}, bearer(token), &proj)

	projID := proj["id"].(string)
	bucketPath := fmt.Sprintf("/api/v1/tenants/%s/projects/%s/buckets", tenant.ID, projID)

	// Create bucket.
	var bucket map[string]any
	resp := ts.doJSON(t, http.MethodPost, bucketPath,
		map[string]string{"name": "docs", "description": "main bucket"},
		bearer(token), &bucket)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusCreated)
	if bucket["name"] != "docs" {
		t.Errorf("name: got %v, want docs", bucket["name"])
	}

	// List.
	var list map[string]any
	resp2 := ts.doJSON(t, http.MethodGet, bucketPath, nil, bearer(token), &list)
	defer resp2.Body.Close()
	assertStatus(t, resp2, http.StatusOK)
	data := list["data"].([]any)
	if len(data) == 0 {
		t.Error("expected at least one bucket")
	}
}
