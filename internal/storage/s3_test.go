package storage_test

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"

	"tinydm/internal/storage"
)

const (
	fakeRegion = "us-east-1"
	fakeBucket = "tinydm-test"
)

// newFakeS3Store spins up an in-process S3 fake and returns a Store backed by
// it. No Docker or network access is required. The server is closed when the
// test ends.
func newFakeS3Store(t *testing.T) *storage.S3Store {
	t.Helper()

	backend := s3mem.New()
	// WithAutoBucket means the fake will auto-create buckets on first use,
	// so we don't need a separate CreateBucket call before uploading.
	faker := gofakes3.New(backend, gofakes3.WithAutoBucket(true))
	srv := httptest.NewServer(faker.Server())
	t.Cleanup(srv.Close)

	store, err := storage.NewS3(fakeBucket, fakeRegion, srv.URL, "fakekey", "fakesecret")
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	return store
}

var ctx = context.Background()

// ─── Put ──────────────────────────────────────────────────────────────────────

func TestS3Put_ReturnsKey(t *testing.T) {
	s := newFakeS3Store(t)
	key, size, checksum, err := s.Put(ctx, strings.NewReader("hello world"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if key == "" {
		t.Error("expected non-empty key")
	}
	if size != int64(len("hello world")) {
		t.Errorf("size: got %d, want %d", size, len("hello world"))
	}
	if len(checksum) != 64 {
		t.Errorf("checksum length: got %d, want 64 (SHA-256 hex)", len(checksum))
	}
}

func TestS3Put_KeyFormat(t *testing.T) {
	s := newFakeS3Store(t)
	key, _, _, err := s.Put(ctx, strings.NewReader("test content"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Key must be "<2-char-prefix>/<62-char-remainder>".
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

func TestS3Put_KeyMatchesChecksum(t *testing.T) {
	s := newFakeS3Store(t)
	key, _, checksum, err := s.Put(ctx, strings.NewReader("deterministic"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	expected := checksum[:2] + "/" + checksum[2:]
	if key != expected {
		t.Errorf("key %q does not match checksum-derived path %q", key, expected)
	}
}

func TestS3Put_Deterministic(t *testing.T) {
	s := newFakeS3Store(t)
	content := "same content every time"
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

func TestS3Put_DifferentContent_DifferentKeys(t *testing.T) {
	s := newFakeS3Store(t)
	k1, _, _, err := s.Put(ctx, strings.NewReader("content A"))
	if err != nil {
		t.Fatalf("Put A: %v", err)
	}
	k2, _, _, err := s.Put(ctx, strings.NewReader("content B"))
	if err != nil {
		t.Fatalf("Put B: %v", err)
	}
	if k1 == k2 {
		t.Error("different content should produce different keys")
	}
}

func TestS3Put_EmptyContent(t *testing.T) {
	s := newFakeS3Store(t)
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

func TestS3Get_ReadsBackContent(t *testing.T) {
	s := newFakeS3Store(t)
	original := "round-trip content"
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

func TestS3Get_BinaryContent(t *testing.T) {
	s := newFakeS3Store(t)
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

func TestS3Get_NonExistentKey(t *testing.T) {
	s := newFakeS3Store(t)
	_, err := s.Get(ctx, "ab/nonexistentkey123456789012345678901234567890123456789012")
	if err == nil {
		t.Error("expected error for non-existent key, got nil")
	}
}

// ─── Delete ───────────────────────────────────────────────────────────────────

func TestS3Delete_RemovesContent(t *testing.T) {
	s := newFakeS3Store(t)
	key, _, _, err := s.Put(ctx, strings.NewReader("delete me"))
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

func TestS3Delete_Idempotent(t *testing.T) {
	s := newFakeS3Store(t)
	key, _, _, err := s.Put(ctx, strings.NewReader("delete twice"))
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

func TestS3Delete_NonExistentKey(t *testing.T) {
	s := newFakeS3Store(t)
	err := s.Delete(ctx, "ab/doesnotexist0000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Errorf("Delete of non-existent key should not error, got: %v", err)
	}
}

// ─── Deduplication ────────────────────────────────────────────────────────────

func TestS3Put_Deduplication(t *testing.T) {
	s := newFakeS3Store(t)
	content := "deduplicated content"

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

func TestS3Put_DeleteThenPutSameContent(t *testing.T) {
	s := newFakeS3Store(t)
	content := "put, delete, put again"

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
