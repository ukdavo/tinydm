package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"tinydm/internal/audit"
	"tinydm/internal/auth"
	"tinydm/internal/config"
	"tinydm/internal/db"
	"tinydm/internal/repo"
	"tinydm/internal/storage"
	"tinydm/internal/web"
)

func init() {
	auth.BCryptCost = bcrypt.MinCost
}

const webTestJWTSecret = "web-test-secret"

// newWebServer spins up a fully-wired web handler backed by an isolated
// in-memory SQLite DB. Returns the server, auth store, seeded tenant, seeded
// user, and a session cookie value (JWT) for that user.
func newWebServer(t *testing.T) (*httptest.Server, *auth.Store, *repo.Tenant, *auth.User, string) {
	t.Helper()

	tmpDir := t.TempDir()
	sqlDB, err := db.Open("sqlite", filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	cfg := &config.Config{
		JWTSecret:        webTestJWTSecret,
		JWTExpiryMinutes: 60,
	}

	authStore := auth.NewStore(sqlDB)
	repoStore := repo.NewStore(sqlDB)
	auditStore := audit.NewStore(sqlDB)
	fileStore, err := storage.NewLocal(filepath.Join(tmpDir, "content"))
	if err != nil {
		t.Fatalf("storage.NewLocal: %v", err)
	}

	ctx := context.Background()
	tenant, err := repoStore.CreateTenant(ctx, "Test", "test tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	hash, _ := auth.HashPassword("adminpass")
	user, err := authStore.CreateUser(ctx, tenant.ID, "alice", "alice@test.local", "Alice", "Smith", hash, auth.UserTypeAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	token, err := auth.NewJWT(webTestJWTSecret, 60, user.ID, user.TenantID, user.Username, user.UserType)
	if err != nil {
		t.Fatalf("NewJWT: %v", err)
	}

	h := web.New(cfg, repoStore, authStore, auditStore, fileStore)
	r := chi.NewRouter()
	web.RegisterRoutes(r, h)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, authStore, tenant, user, token
}

// sessionReq builds a request with the session cookie set.
func sessionReq(t *testing.T, method, rawURL, token string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "tdm_session", Value: token})
	return req
}

// TestAuditLog_ScopedToTenant verifies that GET /admin/tenants/{tenantID}/audit
// returns a 200 for the user's own tenant and that the page exists at the
// tenant-scoped URL (not the old global /admin/audit).
func TestAuditLog_ScopedToTenant(t *testing.T) {
	srv, _, tenant, _, token := newWebServer(t)

	req := sessionReq(t, http.MethodGet, srv.URL+"/admin/tenants/"+tenant.ID+"/audit", token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET audit: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
}

// TestAuditLog_GlobalRouteGone verifies the old /admin/audit route no longer exists.
func TestAuditLog_GlobalRouteGone(t *testing.T) {
	srv, _, _, _, token := newWebServer(t)

	req := sessionReq(t, http.MethodGet, srv.URL+"/admin/audit", token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /admin/audit: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Errorf("expected /admin/audit to be gone (not 200), got %d", resp.StatusCode)
	}
}

// TestAuditLog_CrossTenantForbidden verifies an admin cannot view another tenant's audit log.
func TestAuditLog_CrossTenantForbidden(t *testing.T) {
	srv, _, _, _, token := newWebServer(t)

	// Use a plausible-but-wrong tenant ID; the handler must reject it.
	fakeID := "00000000-0000-0000-0000-000000000000"
	req := sessionReq(t, http.MethodGet, srv.URL+"/admin/tenants/"+fakeID+"/audit", token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET cross-tenant audit: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Errorf("expected non-200 for cross-tenant audit, got %d", resp.StatusCode)
	}
}

// TestPasswordForm_ReturnsInputFragment verifies that GET /admin/users/{id}/password-form
// returns an HTML fragment containing a password input for the user.
func TestPasswordForm_ReturnsInputFragment(t *testing.T) {
	srv, _, _, user, token := newWebServer(t)

	req := sessionReq(t, http.MethodGet, srv.URL+"/admin/users/"+user.ID+"/password-form", token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET password-form: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)

	if !strings.Contains(body, `type="password"`) {
		t.Errorf("expected password input in response, got:\n%s", body)
	}
	if !strings.Contains(body, user.ID) {
		t.Errorf("expected user ID %q in response, got:\n%s", user.ID, body)
	}
}

// TestPasswordChange_Web_ReturnsUserRow verifies that POST /admin/users/{id}/password
// returns the refreshed user-row HTML so HTMX can swap the form row back.
func TestPasswordChange_Web_ReturnsUserRow(t *testing.T) {
	srv, _, _, user, token := newWebServer(t)

	form := url.Values{"password": {"newpassword"}}
	req := sessionReq(t, http.MethodPost, srv.URL+"/admin/users/"+user.ID+"/password", token,
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST password: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)

	if !strings.Contains(body, user.Username) {
		t.Errorf("expected username %q in user-row response, got:\n%s", user.Username, body)
	}
	if !strings.Contains(body, `id="user-`+user.ID+`"`) {
		t.Errorf("expected row id in response, got:\n%s", body)
	}
}

// TestUsersPage_Renders verifies that GET /admin/tenants/{tenantID}/users
// returns 200 and includes the perm_mode card and no template errors.
func TestUsersPage_Renders(t *testing.T) {
	srv, _, tenant, _, token := newWebServer(t)

	req := sessionReq(t, http.MethodGet, srv.URL+"/admin/tenants/"+tenant.ID+"/users", token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET users: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	page := string(body)

	if !strings.Contains(page, "Access policy") {
		t.Error("expected perm_mode card (\"Access policy\") in users page")
	}
	if !strings.Contains(page, "perm-mode-"+tenant.ID) {
		t.Errorf("expected perm_mode select id in page, tenant: %s", tenant.ID)
	}
}
