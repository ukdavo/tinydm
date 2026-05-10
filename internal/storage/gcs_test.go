package storage_test

// GCS integration tests using fake-gcs-server — a pure-Go, in-process GCS
// emulator. No Docker or network access is required; the fake server starts
// and stops within each test binary run.
//
// To run just these tests:
//
//	go test ./internal/storage/ -run TestGCS -v

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/fsouza/fake-gcs-server/fakestorage"

	"tinydm/internal/storage"
)

const (
	gcsFakeBucket  = "tinydm-test"
	gcsFakeProject = "test-project"
)

// newGCSStore spins up an in-process GCS fake and returns a GCSStore backed
// by it. The server is closed when the test binary exits.
func newGCSStore(t *testing.T) *storage.GCSStore {
	t.Helper()

	srv, err := fakestorage.NewServerWithOptions(fakestorage.Options{
		InitialObjects: []fakestorage.Object{},
		Host:           "127.0.0.1",
		Port:           0, // random available port
	})
	if err != nil {
		t.Fatalf("start fake GCS server: %v", err)
	}
	t.Cleanup(srv.Stop)

	client := srv.Client()

	store, err := storage.NewGCSWithClient(client, gcsFakeBucket, gcsFakeProject)
	if err != nil {
		t.Fatalf("NewGCSWithClient: %v", err)
	}
	return store
}

// ─── Put ──────────────────────────────────────────────────────────────────────

func TestGCSPut_ReturnsKey(t *testing.T) {
	s := newGCSStore(t)
	key, size, checksum, err := s.Put(ctx, strings.NewReader("hello gcs"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if key == "" {
		t.Error("expected non-empty key")
	}
	if size != int64(len("hello gcs")) {
		t.Errorf("size: got %d, want %d", size, len("hello gcs"))
	}
	if len(checksum) != 64 {
		t.Errorf("checksum length: got %d, want 64 (SHA-256 hex)", len(checksum))
	}
}

func TestGCSPut_KeyFormat(t *testing.T) {
	s := newGCSStore(t)
	key, _, _, err := s.Put(ctx, strings.NewReader("gcs key format test"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	parts := strings.SplitN(key, "/", 2)
	if len(parts) != 2 {
		t.Fatalf("key %q does not contain '/'", key)
	}
	if len(parts[0]) != 2 {
		t.Errorf("key prefix length: got %d, want 2", len(parts[0]))
	}
	if len(parts[1]) != 62 {
		t.Errorf("key suffix length: got %d, want 62", len(parts[1]))
	}
}

func TestGCSPut_KeyMatchesChecksum(t *testing.T) {
	s := newGCSStore(t)
	key, _, checksum, err := s.Put(ctx, strings.NewReader("deterministic gcs"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	expected := checksum[:2] + "/" + checksum[2:]
	if key != expected {
		t.Errorf("key %q does not match checksum-derived path %q", key, expected)
	}
}

func TestGCSPut_Deterministic(t *testing.T) {
	s := newGCSStore(t)
	content := "same content gcs"
	k1, _, cs1, err := s.Put(ctx, strings.NewReader(content))
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}
	k2, _, cs2, err := s.Put(ctx, strings.NewReader(content))
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if k1 != k2 {
		t.Errorf("same content produced different keys: %q vs %q", k1, k2)
	}
	if cs1 != cs2 {
		t.Errorf("same content produced different checksums: %q vs %q", cs1, cs2)
	}
}

func TestGCSPut_DifferentContent_DifferentKeys(t *testing.T) {
	s := newGCSStore(t)
	k1, _, _, err := s.Put(ctx, strings.NewReader("gcs content A"))
	if err != nil {
		t.Fatalf("Put A: %v", err)
	}
	k2, _, _, err := s.Put(ctx, strings.NewReader("gcs content B"))
	if err != nil {
		t.Fatalf("Put B: %v", err)
	}
	if k1 == k2 {
		t.Error("different content should produce different keys")
	}
}

func TestGCSPut_EmptyContent(t *testing.T) {
	s := newGCSStore(t)
	key, size, checksum, err := s.Put(ctx, strings.NewReader(""))
	if err != nil {
		t.Fatalf("Put empty: %v", err)
	}
	if size != 0 {
		t.Errorf("size: got %d, want 0", size)
	}
	if key == "" || checksum == "" {
		t.Error("expected non-empty key and checksum for empty content")
	}
}

// ─── Get ──────────────────────────────────────────────────────────────────────

func TestGCSGet_ReadsBackContent(t *testing.T) {
	s := newGCSStore(t)
	original := "round-trip gcs content"
	key, _, _, err := s.Put(ctx, strings.NewReader(original))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != original {
		t.Errorf("content: got %q, want %q", got, original)
	}
}

func TestGCSGet_BinaryContent(t *testing.T) {
	s := newGCSStore(t)
	original := []byte{0x00, 0xFF, 0xAB, 0xCD, 0x12, 0x34}
	key, _, _, err := s.Put(ctx, bytes.NewReader(original))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("binary content mismatch")
	}
}

func TestGCSGet_NonExistentKey(t *testing.T) {
	s := newGCSStore(t)
	_, err := s.Get(ctx, "ab/nonexistentkey123456789012345678901234567890123456789012")
	if err == nil {
		t.Error("expected error for non-existent key, got nil")
	}
}

// ─── Delete ───────────────────────────────────────────────────────────────────

func TestGCSDelete_RemovesContent(t *testing.T) {
	s := newGCSStore(t)
	key, _, _, err := s.Put(ctx, strings.NewReader("delete me gcs"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = s.Get(ctx, key)
	if err == nil {
		t.Error("expected error after deleting content, got nil")
	}
}

func TestGCSDelete_Idempotent(t *testing.T) {
	s := newGCSStore(t)
	key, _, _, err := s.Put(ctx, strings.NewReader("delete twice gcs"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Errorf("second Delete should be idempotent, got: %v", err)
	}
}

func TestGCSDelete_NonExistentKey(t *testing.T) {
	s := newGCSStore(t)
	err := s.Delete(ctx, "ab/doesnotexist0000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Errorf("Delete of non-existent key should not error, got: %v", err)
	}
}

// ─── Deduplication ────────────────────────────────────────────────────────────

func TestGCSPut_Deduplication(t *testing.T) {
	s := newGCSStore(t)
	content := "deduplicated gcs content"

	key1, size1, cs1, err := s.Put(ctx, strings.NewReader(content))
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}
	key2, size2, cs2, err := s.Put(ctx, strings.NewReader(content))
	if err != nil {
		t.Fatalf("second Put (dedup): %v", err)
	}

	if key1 != key2 || size1 != size2 || cs1 != cs2 {
		t.Error("duplicate content should return identical key/size/checksum")
	}

	rc, err := s.Get(ctx, key1)
	if err != nil {
		t.Fatalf("Get after dedup: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != content {
		t.Errorf("dedup content: got %q, want %q", got, content)
	}
}

func TestGCSPut_DeleteThenPutSameContent(t *testing.T) {
	s := newGCSStore(t)
	content := "put, delete, put again gcs"

	key, _, _, err := s.Put(ctx, strings.NewReader(content))
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	key2, _, _, err := s.Put(ctx, strings.NewReader(content))
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if key != key2 {
		t.Errorf("re-put key %q differs from original %q", key2, key)
	}
	rc, err := s.Get(ctx, key2)
	if err != nil {
		t.Fatalf("Get after re-put: %v", err)
	}
	rc.Close()
}
