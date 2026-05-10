package storage_test

// Azure Blob Storage integration tests.
//
// These tests require a running Azurite instance (the official Azure Blob
// emulator). They are skipped automatically unless TINYDM_AZURE_TEST_ENDPOINT
// is set. To run them locally:
//
//	docker run -p 10000:10000 mcr.microsoft.com/azure-storage/azurite \
//	    azurite-blob --blobHost 0.0.0.0
//
//	TINYDM_AZURE_TEST_ENDPOINT=http://localhost:10000 \
//	    go test ./internal/storage/ -run TestAzure -v

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"tinydm/internal/storage"
)

const (
	azureTestAccount   = "devstoreaccount1"              // Azurite default account
	azureTestKey       = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==" // Azurite default key
	azureTestContainer = "tinydm-test"
)

// newAzureStore creates an AzureStore pointed at Azurite and skips the test
// if TINYDM_AZURE_TEST_ENDPOINT is not set.
func newAzureStore(t *testing.T) *storage.AzureStore {
	t.Helper()

	endpoint := os.Getenv("TINYDM_AZURE_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set TINYDM_AZURE_TEST_ENDPOINT to run Azure Blob integration tests")
	}

	store, err := storage.NewAzure(azureTestAccount, azureTestKey, azureTestContainer, endpoint)
	if err != nil {
		t.Fatalf("NewAzure: %v", err)
	}
	return store
}

// ─── Put ──────────────────────────────────────────────────────────────────────

func TestAzurePut_ReturnsKey(t *testing.T) {
	s := newAzureStore(t)
	key, size, checksum, err := s.Put(ctx, strings.NewReader("hello azure"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if key == "" {
		t.Error("expected non-empty key")
	}
	if size != int64(len("hello azure")) {
		t.Errorf("size: got %d, want %d", size, len("hello azure"))
	}
	if len(checksum) != 64 {
		t.Errorf("checksum length: got %d, want 64 (SHA-256 hex)", len(checksum))
	}
}

func TestAzurePut_KeyFormat(t *testing.T) {
	s := newAzureStore(t)
	key, _, _, err := s.Put(ctx, strings.NewReader("key format test"))
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

func TestAzurePut_KeyMatchesChecksum(t *testing.T) {
	s := newAzureStore(t)
	key, _, checksum, err := s.Put(ctx, strings.NewReader("deterministic azure"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	expected := checksum[:2] + "/" + checksum[2:]
	if key != expected {
		t.Errorf("key %q does not match checksum-derived path %q", key, expected)
	}
}

func TestAzurePut_Deterministic(t *testing.T) {
	s := newAzureStore(t)
	content := "same content azure"
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

func TestAzurePut_DifferentContent_DifferentKeys(t *testing.T) {
	s := newAzureStore(t)
	k1, _, _, err := s.Put(ctx, strings.NewReader("azure content A"))
	if err != nil {
		t.Fatalf("Put A: %v", err)
	}
	k2, _, _, err := s.Put(ctx, strings.NewReader("azure content B"))
	if err != nil {
		t.Fatalf("Put B: %v", err)
	}
	if k1 == k2 {
		t.Error("different content should produce different keys")
	}
}

// ─── Get ──────────────────────────────────────────────────────────────────────

func TestAzureGet_ReadsBackContent(t *testing.T) {
	s := newAzureStore(t)
	original := "round-trip azure content"
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

func TestAzureGet_BinaryContent(t *testing.T) {
	s := newAzureStore(t)
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

func TestAzureGet_NonExistentKey(t *testing.T) {
	s := newAzureStore(t)
	_, err := s.Get(ctx, "ab/nonexistentkey123456789012345678901234567890123456789012")
	if err == nil {
		t.Error("expected error for non-existent key, got nil")
	}
}

// ─── Delete ───────────────────────────────────────────────────────────────────

func TestAzureDelete_RemovesContent(t *testing.T) {
	s := newAzureStore(t)
	key, _, _, err := s.Put(ctx, strings.NewReader("delete me azure"))
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

func TestAzureDelete_Idempotent(t *testing.T) {
	s := newAzureStore(t)
	key, _, _, err := s.Put(ctx, strings.NewReader("delete twice azure"))
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

func TestAzureDelete_NonExistentKey(t *testing.T) {
	s := newAzureStore(t)
	err := s.Delete(ctx, "ab/doesnotexist0000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Errorf("Delete of non-existent key should not error, got: %v", err)
	}
}

// ─── Deduplication ────────────────────────────────────────────────────────────

func TestAzurePut_Deduplication(t *testing.T) {
	s := newAzureStore(t)
	content := "deduplicated azure content"

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
