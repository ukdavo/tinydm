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
	"tinydm/internal/cluster"
	"tinydm/internal/config"
	"tinydm/internal/db"
	"tinydm/internal/repo"
	"tinydm/internal/storage"
)

// benchServer mirrors testServer but accepts *testing.B so it can be used in
// benchmark functions directly. Setup cost is excluded from b.N iterations by
// calling b.ResetTimer() after seeding.
type benchServer struct {
	srv      *httptest.Server
	token    string // pre-issued Bearer token for the seeded admin
	tenantID string
	projectID string
	bucketID  string
	repoStore *repo.Store
}

func newBenchServer(b *testing.B) *benchServer {
	b.Helper()

	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")

	sqlDB, err := db.Open("sqlite", dbPath)
	if err != nil {
		b.Fatalf("db.Open: %v", err)
	}
	b.Cleanup(func() { sqlDB.Close() })

	if err := db.Migrate(sqlDB); err != nil {
		b.Fatalf("db.Migrate: %v", err)
	}

	cfg := &config.Config{
		DBDriver:         "sqlite",
		JWTSecret:        testJWTSecret,
		JWTExpiryMinutes: 60,
	}

	authStore := auth.NewStore(sqlDB)
	repoStore := repo.NewStore(sqlDB)
	auditStore := audit.NewStore(sqlDB)

	fileStore, err := storage.NewLocal(filepath.Join(tmpDir, "content"))
	if err != nil {
		b.Fatalf("storage.NewLocal: %v", err)
	}

	r := chi.NewRouter()
	r.Use(auth.Authenticator(cfg.JWTSecret, authStore))
	api.RegisterRoutes(r, cfg, repoStore, authStore, fileStore, auditStore, cluster.NewNoOpLocker())

	srv := httptest.NewServer(r)
	b.Cleanup(srv.Close)

	ctx := context.Background()

	// Seed: tenant
	tenant, err := repoStore.CreateTenant(ctx, "bench-tenant", "benchmark tenant")
	if err != nil {
		b.Fatalf("CreateTenant: %v", err)
	}

	// Seed: admin user
	hash, err := auth.HashPassword("bench-pass")
	if err != nil {
		b.Fatalf("HashPassword: %v", err)
	}
	_, err = authStore.CreateUser(ctx, "bench-admin", "bench@test.local", "Bench", "Admin", hash, auth.UserTypeAdmin)
	if err != nil {
		b.Fatalf("CreateUser: %v", err)
	}

	// Obtain JWT for subsequent benchmark requests
	token, err := auth.NewJWT(cfg.JWTSecret, cfg.JWTExpiryMinutes,
		"bench-user", "bench-admin", auth.UserTypeAdmin)
	if err != nil {
		b.Fatalf("NewJWT: %v", err)
	}

	// Seed: project and bucket
	proj, err := repoStore.CreateProject(ctx, tenant.ID, "bench-project", "")
	if err != nil {
		b.Fatalf("CreateProject: %v", err)
	}
	bucket, err := repoStore.CreateBucket(ctx, proj.ID, "bench-bucket", "")
	if err != nil {
		b.Fatalf("CreateBucket: %v", err)
	}

	return &benchServer{
		srv:       srv,
		token:     token,
		tenantID:  tenant.ID,
		projectID: proj.ID,
		bucketID:  bucket.ID,
		repoStore: repoStore,
	}
}

func (bs *benchServer) url(path string) string {
	return bs.srv.URL + path
}

func (bs *benchServer) bearerHeader() map[string]string {
	return map[string]string{"Authorization": "Bearer " + bs.token}
}

// doB executes a request inside a benchmark, calling b.Fatal on error.
func (bs *benchServer) doB(b *testing.B, method, path string, body io.Reader, headers map[string]string) *http.Response {
	b.Helper()
	req, err := http.NewRequest(method, bs.url(path), body)
	if err != nil {
		b.Fatalf("http.NewRequest %s %s: %v", method, path, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.Fatalf("http do %s %s: %v", method, path, err)
	}
	return resp
}

// uploadBench posts a multipart upload and returns the document ID.
func (bs *benchServer) uploadBench(b *testing.B, filename string, content []byte) string {
	b.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		b.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		b.Fatalf("write form file: %v", err)
	}
	mw.Close()

	path := fmt.Sprintf("/api/v1/tenants/%s/projects/%s/buckets/%s/documents",
		bs.tenantID, bs.projectID, bs.bucketID)
	headers := map[string]string{
		"Authorization": "Bearer " + bs.token,
		"Content-Type":  mw.FormDataContentType(),
	}
	resp := bs.doB(b, http.MethodPost, path, &buf, headers)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		b.Fatalf("upload: got %d: %s", resp.StatusCode, body)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		b.Fatalf("decode upload response: %v", err)
	}
	return doc["id"].(string)
}

// ─── Login ────────────────────────────────────────────────────────────────────

// BenchmarkLogin measures the full POST /api/v1/auth/login round trip,
// including bcrypt comparison (MinCost in test builds) and JWT issuance.
func BenchmarkLogin(b *testing.B) {
	bs := newBenchServer(b)
	body := map[string]string{
		"tenant_id": bs.tenantID,
		"username":  "bench-admin",
		"password":  "bench-pass",
	}
	bodyBytes, _ := json.Marshal(body)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp := bs.doB(b, http.MethodPost, "/api/v1/auth/login",
			bytes.NewReader(bodyBytes),
			map[string]string{"Content-Type": "application/json"},
		)
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("login: got %d", resp.StatusCode)
		}
	}
}

// ─── Document upload ──────────────────────────────────────────────────────────

// BenchmarkDocumentUpload measures multipart file upload at several payload
// sizes. Each iteration uploads a unique payload (byte 0 varies) so the
// dedup fast path is not taken, exercising SHA-256 hashing + disk write.
func BenchmarkDocumentUpload(b *testing.B) {
	sizes := []struct {
		label string
		bytes int
	}{
		{"1KB", 1 << 10},
		{"64KB", 64 << 10},
		{"1MB", 1 << 20},
	}

	for _, tc := range sizes {
		tc := tc
		b.Run(tc.label, func(b *testing.B) {
			bs := newBenchServer(b)
			payload := bytes.Repeat([]byte("u"), tc.bytes)
			uploadPath := fmt.Sprintf("/api/v1/tenants/%s/projects/%s/buckets/%s/documents",
				bs.tenantID, bs.projectID, bs.bucketID)

			b.SetBytes(int64(tc.bytes))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				payload[0] = byte(i) // unique content → skip dedup

				var buf bytes.Buffer
				mw := multipart.NewWriter(&buf)
				fw, err := mw.CreateFormFile("file", fmt.Sprintf("bench-%d.bin", i))
				if err != nil {
					b.Fatal(err)
				}
				if _, err := fw.Write(payload); err != nil {
					b.Fatal(err)
				}
				mw.Close()

				headers := map[string]string{
					"Authorization": "Bearer " + bs.token,
					"Content-Type":  mw.FormDataContentType(),
				}
				resp := bs.doB(b, http.MethodPost, uploadPath, &buf, headers)
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusCreated {
					b.Fatalf("upload: got %d", resp.StatusCode)
				}
			}
		})
	}
}

// ─── Document list ────────────────────────────────────────────────────────────

// BenchmarkDocumentList measures GET .../documents with varying numbers of
// pre-seeded documents to capture how list performance scales with row count.
func BenchmarkDocumentList(b *testing.B) {
	counts := []struct {
		label string
		n     int
	}{
		{"10docs", 10},
		{"100docs", 100},
	}

	for _, tc := range counts {
		tc := tc
		b.Run(tc.label, func(b *testing.B) {
			bs := newBenchServer(b)

			// Pre-seed documents (setup, excluded from timer).
			for i := 0; i < tc.n; i++ {
				content := []byte(fmt.Sprintf("doc content %d", i))
				bs.uploadBench(b, fmt.Sprintf("doc%d.txt", i), content)
			}

			listPath := fmt.Sprintf("/api/v1/tenants/%s/projects/%s/buckets/%s/documents",
				bs.tenantID, bs.projectID, bs.bucketID)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resp := bs.doB(b, http.MethodGet, listPath, nil, bs.bearerHeader())
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					b.Fatalf("list: got %d", resp.StatusCode)
				}
			}
		})
	}
}

// ─── Document download ────────────────────────────────────────────────────────

// BenchmarkDocumentDownload measures GET .../documents/{id}/content for a
// pre-uploaded file. The response body is fully drained to include HTTP
// chunked-transfer and file-read cost.
func BenchmarkDocumentDownload(b *testing.B) {
	sizes := []struct {
		label string
		bytes int
	}{
		{"1KB", 1 << 10},
		{"1MB", 1 << 20},
	}

	for _, tc := range sizes {
		tc := tc
		b.Run(tc.label, func(b *testing.B) {
			bs := newBenchServer(b)

			// Upload the file once; reuse the same content on every iteration
			// (exercises dedup fast-path on disk; open+stream is the hot path).
			payload := bytes.Repeat([]byte("d"), tc.bytes)
			docID := bs.uploadBench(b, "bench.bin", payload)

			downloadPath := fmt.Sprintf("/api/v1/tenants/%s/projects/%s/buckets/%s/documents/%s/content",
				bs.tenantID, bs.projectID, bs.bucketID, docID)

			b.SetBytes(int64(tc.bytes))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resp := bs.doB(b, http.MethodGet, downloadPath, nil, bs.bearerHeader())
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					b.Fatalf("download: got %d", resp.StatusCode)
				}
			}
		})
	}
}
