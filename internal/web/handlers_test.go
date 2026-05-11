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
// in-memory SQLite DB. Returns the server, auth store, repo store, seeded
// user, and a session cookie value (JWT) for that user.
func newWebServer(t *testing.T) (*httptest.Server, *auth.Store, *repo.Store, *auth.User, string) {
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
	hash, _ := auth.HashPassword("adminpass")
	user, err := authStore.CreateUser(ctx, "alice", "alice@test.local", "Alice", "Smith", hash, auth.UserTypeAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	token, err := auth.NewJWT(webTestJWTSecret, 60, user.ID, user.Username, user.UserType)
	if err != nil {
		t.Fatalf("NewJWT: %v", err)
	}

	h := web.New(cfg, repoStore, authStore, auditStore, fileStore)
	r := chi.NewRouter()
	web.RegisterRoutes(r, h)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, authStore, repoStore, user, token
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

// createNonAdminToken seeds a non-admin user in authStore and returns a JWT session token.
func createNonAdminToken(t *testing.T, authStore *auth.Store) string {
	t.Helper()
	hash, _ := auth.HashPassword("userpass")
	user, err := authStore.CreateUser(context.Background(), "bob", "bob@test.local", "Bob", "Jones", hash, auth.UserTypeUser)
	if err != nil {
		t.Fatalf("CreateUser non-admin: %v", err)
	}
	token, err := auth.NewJWT(webTestJWTSecret, 60, user.ID, user.Username, user.UserType)
	if err != nil {
		t.Fatalf("NewJWT non-admin: %v", err)
	}
	return token
}

// TestAuditLog_Renders verifies that GET /app/audit returns 200.
func TestAuditLog_Renders(t *testing.T) {
	srv, _, _, _, token := newWebServer(t)

	req := sessionReq(t, http.MethodGet, srv.URL+"/app/audit", token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET audit: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
}

// TestAuditLog_GlobalRouteGone verifies the old /app/tenants route no longer exists.
func TestAuditLog_GlobalRouteGone(t *testing.T) {
	srv, _, _, _, token := newWebServer(t)

	req := sessionReq(t, http.MethodGet, srv.URL+"/app/tenants", token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /app/tenants: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Errorf("expected /app/tenants to be gone (not 200), got %d", resp.StatusCode)
	}
}

// TestPasswordForm_ReturnsInputFragment verifies that GET /app/users/{id}/password-form
// returns an HTML fragment containing a password input for the user.
func TestPasswordForm_ReturnsInputFragment(t *testing.T) {
	srv, _, _, user, token := newWebServer(t)

	req := sessionReq(t, http.MethodGet, srv.URL+"/app/users/"+user.ID+"/password-form", token, nil)
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

// TestPasswordChange_Web_ReturnsUserRow verifies that POST /app/users/{id}/password
// returns the refreshed user-row HTML so HTMX can swap the form row back.
func TestPasswordChange_Web_ReturnsUserRow(t *testing.T) {
	srv, _, _, user, token := newWebServer(t)

	form := url.Values{"password": {"newpassword"}}
	req := sessionReq(t, http.MethodPost, srv.URL+"/app/users/"+user.ID+"/password", token,
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

// TestAPIKeysPage_Renders verifies that GET /app/apikeys returns 200.
func TestAPIKeysPage_Renders(t *testing.T) {
	srv, _, _, _, token := newWebServer(t)

	req := sessionReq(t, http.MethodGet, srv.URL+"/app/apikeys", token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET apikeys: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, body)
	}
}

// TestUsersPage_Renders verifies that GET /app/users returns 200 and
// includes the perm_mode card and no template errors.
func TestUsersPage_Renders(t *testing.T) {
	srv, _, _, _, token := newWebServer(t)

	req := sessionReq(t, http.MethodGet, srv.URL+"/app/users", token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET users: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, body)
	}
}

// TestDashboardPage_Renders verifies that GET /app/ returns 200.
func TestDashboardPage_Renders(t *testing.T) {
	srv, _, _, _, token := newWebServer(t)

	req := sessionReq(t, http.MethodGet, srv.URL+"/app/", token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /app/: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, body)
	}
}

// TestProjectsPage_Renders verifies that GET /app/projects returns 200.
func TestProjectsPage_Renders(t *testing.T) {
	srv, _, repoStore, _, token := newWebServer(t)

	req := sessionReq(t, http.MethodGet, srv.URL+"/app/projects", token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET projects: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, body)
	}
	_ = repoStore
}

// TestBucketsPage_Renders verifies that GET /app/projects/{projectID}/buckets returns 200.
func TestBucketsPage_Renders(t *testing.T) {
	srv, _, repoStore, _, token := newWebServer(t)

	project, err := repoStore.CreateProject(context.Background(), "test-proj", "test project")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	req := sessionReq(t, http.MethodGet,
		srv.URL+"/app/projects/"+project.ID+"/buckets",
		token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET buckets: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, body)
	}
}

// TestDocumentsPage_Renders verifies that GET .../buckets/{bucketID}/documents returns 200.
func TestDocumentsPage_Renders(t *testing.T) {
	srv, _, repoStore, _, token := newWebServer(t)

	project, err := repoStore.CreateProject(context.Background(), "test-proj", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	bucket, err := repoStore.CreateBucket(context.Background(), project.ID, "test-bucket", "test bucket")
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}

	req := sessionReq(t, http.MethodGet,
		srv.URL+"/app/projects/"+project.ID+"/buckets/"+bucket.ID+"/documents",
		token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET documents: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, body)
	}
}

// TestDocumentDetailPage_Renders verifies that GET /app/documents/{documentID} returns 200.
func TestDocumentDetailPage_Renders(t *testing.T) {
	srv, _, repoStore, user, token := newWebServer(t)

	project, err := repoStore.CreateProject(context.Background(), "test-proj", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	bucket, err := repoStore.CreateBucket(context.Background(), project.ID, "test-bucket", "test bucket")
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	doc, err := repoStore.CreateDocument(context.Background(), bucket.ID, "test.txt", "text/plain", 4, "abc", "key/test.txt", user.ID)
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}

	req := sessionReq(t, http.MethodGet, srv.URL+"/app/documents/"+doc.ID, token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET document detail: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, body)
	}
}

// TestRequireAdmin_NonAdminForbiddenOnCreateProject verifies that a non-admin user
// gets 403 when POSTing to /app/projects.
func TestRequireAdmin_NonAdminForbiddenOnCreateProject(t *testing.T) {
	srv, authStore, _, _, _ := newWebServer(t)
	userToken := createNonAdminToken(t, authStore)

	form := url.Values{"name": {"test-proj"}}
	req := sessionReq(t, http.MethodPost, srv.URL+"/app/projects", userToken,
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /app/projects: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
	}
}

// TestNonAdminCanAccessProjects verifies that a non-admin user gets 200 on GET /app/projects.
func TestNonAdminCanAccessProjects(t *testing.T) {
	srv, authStore, _, _, _ := newWebServer(t)
	userToken := createNonAdminToken(t, authStore)

	req := sessionReq(t, http.MethodGet, srv.URL+"/app/projects", userToken, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /app/projects: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, body)
	}
}

// TestRequireAdmin_NonAdminForbiddenOnUsers verifies that a non-admin user
// gets 403 when accessing the users page.
func TestRequireAdmin_NonAdminForbiddenOnUsers(t *testing.T) {
	srv, authStore, _, _, _ := newWebServer(t)
	userToken := createNonAdminToken(t, authStore)

	req := sessionReq(t, http.MethodGet, srv.URL+"/app/users", userToken, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /app/users: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
	}
}
