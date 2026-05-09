package storage

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Local {
	t.Helper()
	s, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	return s
}

// ─── Put ──────────────────────────────────────────────────────────────────────

func TestPut_ReturnsKey(t *testing.T) {
	s := newTestStore(t)
	key, size, checksum, err := s.Put(context.Background(), strings.NewReader("hello world"))
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

func TestPut_KeyFormat(t *testing.T) {
	s := newTestStore(t)
	key, _, _, err := s.Put(context.Background(), strings.NewReader("test content"))
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

func TestPut_KeyMatchesChecksum(t *testing.T) {
	s := newTestStore(t)
	key, _, checksum, err := s.Put(context.Background(), strings.NewReader("deterministic"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	// key = checksum[:2] + "/" + checksum[2:]
	expectedKey := checksum[:2] + "/" + checksum[2:]
	if key != expectedKey {
		t.Errorf("key %q does not match checksum-derived path %q", key, expectedKey)
	}
}

func TestPut_Deterministic(t *testing.T) {
	s := newTestStore(t)
	content := "same content every time"
	k1, _, cs1, err := s.Put(context.Background(), strings.NewReader(content))
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}
	k2, _, cs2, err := s.Put(context.Background(), strings.NewReader(content))
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

func TestPut_DifferentContent_DifferentKeys(t *testing.T) {
	s := newTestStore(t)
	k1, _, _, err := s.Put(context.Background(), strings.NewReader("content A"))
	if err != nil {
		t.Fatalf("Put A: %v", err)
	}
	k2, _, _, err := s.Put(context.Background(), strings.NewReader("content B"))
	if err != nil {
		t.Fatalf("Put B: %v", err)
	}
	if k1 == k2 {
		t.Error("different content should produce different keys")
	}
}

func TestPut_EmptyContent(t *testing.T) {
	s := newTestStore(t)
	key, size, checksum, err := s.Put(context.Background(), strings.NewReader(""))
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

func TestGet_ReadsBackContent(t *testing.T) {
	s := newTestStore(t)
	original := "round-trip content"
	key, _, _, err := s.Put(context.Background(), strings.NewReader(original))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, err := s.Get(context.Background(), key)
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

func TestGet_BinaryContent(t *testing.T) {
	s := newTestStore(t)
	original := []byte{0x00, 0xFF, 0xAB, 0xCD, 0x12, 0x34}
	key, _, _, err := s.Put(context.Background(), bytes.NewReader(original))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, err := s.Get(context.Background(), key)
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

func TestGet_NonExistentKey(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), "ab/nonexistentkey123456789012345678901234567890123456789012")
	if err == nil {
		t.Error("expected error for non-existent key, got nil")
	}
}

// ─── Delete ───────────────────────────────────────────────────────────────────

func TestDelete_RemovesContent(t *testing.T) {
	s := newTestStore(t)
	key, _, _, err := s.Put(context.Background(), strings.NewReader("delete me"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.Delete(context.Background(), key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = s.Get(context.Background(), key)
	if err == nil {
		t.Error("expected error after deleting content, got nil")
	}
}

func TestDelete_Idempotent(t *testing.T) {
	s := newTestStore(t)
	key, _, _, err := s.Put(context.Background(), strings.NewReader("delete twice"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.Delete(context.Background(), key); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	// Second delete of the same key should not error.
	if err := s.Delete(context.Background(), key); err != nil {
		t.Errorf("second Delete should be idempotent, got: %v", err)
	}
}

func TestDelete_NonExistentKey(t *testing.T) {
	s := newTestStore(t)
	err := s.Delete(context.Background(), "ab/doesnotexist0000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Errorf("Delete of non-existent key should not error, got: %v", err)
	}
}

// ─── Deduplication ────────────────────────────────────────────────────────────

func TestPut_Deduplication(t *testing.T) {
	s := newTestStore(t)
	content := "deduplicated content"

	key1, size1, cs1, err := s.Put(context.Background(), strings.NewReader(content))
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}
	key2, size2, cs2, err := s.Put(context.Background(), strings.NewReader(content))
	if err != nil {
		t.Fatalf("second Put (dedup): %v", err)
	}

	if key1 != key2 || size1 != size2 || cs1 != cs2 {
		t.Error("duplicate content should return identical key/size/checksum")
	}

	// Content should still be readable after second Put.
	rc, err := s.Get(context.Background(), key1)
	if err != nil {
		t.Fatalf("Get after dedup: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != content {
		t.Errorf("dedup content: got %q, want %q", got, content)
	}
}

func TestPut_DeleteThenPutSameContent(t *testing.T) {
	s := newTestStore(t)
	content := "put, delete, put again"

	key, _, _, err := s.Put(context.Background(), strings.NewReader(content))
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}
	if err := s.Delete(context.Background(), key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Re-putting deleted content should work.
	key2, _, _, err := s.Put(context.Background(), strings.NewReader(content))
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if key != key2 {
		t.Errorf("re-put key %q differs from original %q", key2, key)
	}
	rc, err := s.Get(context.Background(), key2)
	if err != nil {
		t.Fatalf("Get after re-put: %v", err)
	}
	rc.Close()
}
