// Package api_test contains Go-level integration tests for the HTTP API.
// Each test function spins up a fully-wired httptest.Server backed by an
// isolated in-memory SQLite database, so tests are hermetic and can run in
// parallel without interfering with each other or the development database.
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"tinydm/internal/api"
	"tinydm/internal/audit"
	"tinydm/internal/auth"
	"tinydm/internal/config"
	"tinydm/internal/db"
	"tinydm/internal/repo"
	"tinydm/internal/storage"
)

const testJWTSecret = "integration-test-secret"

// testServer wraps an httptest.Server together with the stores needed to seed
// test data directly (bypassing HTTP when convenient).
type testServer struct {
	srv       *httptest.Server
	cfg       *config.Config
	authStore *auth.Store
	repoStore *repo.Store
}

// newTestServer creates a fully-wired test server. The database and file store
// are isolated to a temporary directory that is cleaned up when the test ends.
func newTestServer(t *testing.T) *testServer {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	sqlDB, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	cfg := &config.Config{
		JWTSecret:        testJWTSecret,
		JWTExpiryMinutes: 60,
	}

	authStore := auth.NewStore(sqlDB)
	repoStore := repo.NewStore(sqlDB)
	auditStore := audit.NewStore(sqlDB)

	fileStore, err := storage.NewLocal(filepath.Join(tmpDir, "content"))
	if err != nil {
		t.Fatalf("storage.NewLocal: %v", err)
	}

	r := chi.NewRouter()
	r.Use(auth.Authenticator(cfg.JWTSecret, authStore))
	api.RegisterRoutes(r, cfg, repoStore, authStore, fileStore, auditStore)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &testServer{
		srv:       srv,
		cfg:       cfg,
		authStore: authStore,
		repoStore: repoStore,
	}
}

// url builds a full URL for a given path.
func (ts *testServer) url(path string) string {
	return ts.srv.URL + path
}

// do executes a request and returns the response. The caller must close the body.
func (ts *testServer) do(t *testing.T, method, path string, body io.Reader, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.url(path), body)
	if err != nil {
		t.Fatalf("http.NewRequest %s %s: %v", method, path, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http do %s %s: %v", method, path, err)
	}
	return resp
}

// doJSON sends a JSON request and decodes the response into dst (may be nil).
func (ts *testServer) doJSON(t *testing.T, method, path string, reqBody any, headers map[string]string, dst any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
		if headers == nil {
			headers = map[string]string{}
		}
		headers["Content-Type"] = "application/json"
	}
	resp := ts.do(t, method, path, bodyReader, headers)
	if dst != nil {
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return resp
}

// login authenticates as the given user and returns a Bearer token.
func (ts *testServer) login(t *testing.T, tenantID, username, password string) string {
	t.Helper()
	var result map[string]any
	resp := ts.doJSON(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"tenant_id": tenantID,
		"username":  username,
		"password":  password,
	}, nil, &result)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: got %d, want 200", resp.StatusCode)
	}
	token, ok := result["token"].(string)
	if !ok || token == "" {
		t.Fatalf("login: no token in response: %v", result)
	}
	return token
}

// bearer returns a headers map containing an Authorization: Bearer header.
func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// seedAdminUser creates a tenant and an admin user in the DB, returning both.
func (ts *testServer) seedAdminUser(t *testing.T, tenantName, username, password string) (*repo.Tenant, *auth.User) {
	t.Helper()
	ctx := context.Background()

	tenant, err := ts.repoStore.CreateTenant(ctx, tenantName, "test tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user, err := ts.authStore.CreateUser(ctx, tenant.ID, username, username+"@test.local", hash, auth.UserTypeAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return tenant, user
}

// uploadFile posts a multipart file upload to the documents endpoint and
// returns the decoded document JSON map.
func (ts *testServer) uploadFile(t *testing.T, token, tenantID, projectID, bucketID, filename string, content []byte) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	mw.Close()

	path := fmt.Sprintf("/api/v1/tenants/%s/projects/%s/buckets/%s/documents",
		tenantID, projectID, bucketID)
	resp := ts.do(t, http.MethodPost, path, &buf, map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  mw.FormDataContentType(),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: got %d: %s", resp.StatusCode, body)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	return doc
}

// decodeBody reads and JSON-decodes the response body, closing it afterwards.
func decodeBody(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decodeBody: %v", err)
	}
}

// assertStatus fails the test if the response status does not match want.
func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status: got %d, want %d — body: %s", resp.StatusCode, want, body)
	}
}
