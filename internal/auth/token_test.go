package auth

import (
	"strings"
	"testing"
	"time"
)

const testSecret = "super-secret-test-key"

func TestNewJWT_ParseJWT_RoundTrip(t *testing.T) {
	userID := "user-123"
	username := "alice"
	userType := UserTypeUser

	token, err := NewJWT(testSecret, 60, userID, username, userType)
	if err != nil {
		t.Fatalf("NewJWT: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := ParseJWT(testSecret, token)
	if err != nil {
		t.Fatalf("ParseJWT: %v", err)
	}

	if claims.Subject != userID {
		t.Errorf("Subject: got %q, want %q", claims.Subject, userID)
	}
	if claims.Username != username {
		t.Errorf("Username: got %q, want %q", claims.Username, username)
	}
	if claims.UserType != string(userType) {
		t.Errorf("UserType: got %q, want %q", claims.UserType, userType)
	}
}

func TestNewJWT_AdminType(t *testing.T) {
	token, err := NewJWT(testSecret, 60, "admin-1", "admin", UserTypeAdmin)
	if err != nil {
		t.Fatalf("NewJWT: %v", err)
	}
	claims, err := ParseJWT(testSecret, token)
	if err != nil {
		t.Fatalf("ParseJWT: %v", err)
	}
	if claims.UserType != string(UserTypeAdmin) {
		t.Errorf("UserType: got %q, want %q", claims.UserType, UserTypeAdmin)
	}
}

func TestParseJWT_WrongSecret(t *testing.T) {
	token, err := NewJWT(testSecret, 60, "u1", "bob", UserTypeUser)
	if err != nil {
		t.Fatalf("NewJWT: %v", err)
	}
	_, err = ParseJWT("completely-different-secret", token)
	if err == nil {
		t.Fatal("expected error when parsing with wrong secret, got nil")
	}
}

func TestParseJWT_ExpiredToken(t *testing.T) {
	// Issue a token that expired 1 minute in the past.
	token, err := NewJWT(testSecret, -1, "u1", "bob", UserTypeUser)
	if err != nil {
		t.Fatalf("NewJWT: %v", err)
	}
	_, err = ParseJWT(testSecret, token)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestParseJWT_Malformed(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"empty string", ""},
		{"random garbage", "not.a.jwt"},
		{"truncated", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1MSJ9"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseJWT(testSecret, tc.token)
			if err == nil {
				t.Error("expected error for malformed token, got nil")
			}
		})
	}
}

func TestNewJWT_ExpiryIsInFuture(t *testing.T) {
	token, err := NewJWT(testSecret, 30, "u1", "carol", UserTypeUser)
	if err != nil {
		t.Fatalf("NewJWT: %v", err)
	}
	claims, err := ParseJWT(testSecret, token)
	if err != nil {
		t.Fatalf("ParseJWT: %v", err)
	}
	if !claims.ExpiresAt.Time.After(time.Now()) {
		t.Errorf("expected expiry in the future, got %v", claims.ExpiresAt.Time)
	}
}

// ─── GenerateAPIKey ────────────────────────────────────────────────────────────

func TestGenerateAPIKey_Format(t *testing.T) {
	plaintext, hash, prefix, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}

	if !strings.HasPrefix(plaintext, "tdm_") {
		t.Errorf("plaintext %q does not start with 'tdm_'", plaintext)
	}
	// "tdm_" + 64 hex chars = 68 chars
	if len(plaintext) != 68 {
		t.Errorf("plaintext length: got %d, want 68", len(plaintext))
	}
	// SHA-256 hex = 64 chars
	if len(hash) != 64 {
		t.Errorf("hash length: got %d, want 64", len(hash))
	}
	// prefix = first 12 chars of plaintext ("tdm_" + 8 hex)
	if len(prefix) != 12 {
		t.Errorf("prefix length: got %d, want 12", len(prefix))
	}
	if !strings.HasPrefix(prefix, "tdm_") {
		t.Errorf("prefix %q does not start with 'tdm_'", prefix)
	}
	if plaintext[:12] != prefix {
		t.Errorf("prefix %q does not match first 12 chars of plaintext %q", prefix, plaintext[:12])
	}
}

func TestGenerateAPIKey_Uniqueness(t *testing.T) {
	p1, h1, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("first key: %v", err)
	}
	p2, h2, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("second key: %v", err)
	}
	if p1 == p2 {
		t.Error("two generated keys should not be equal")
	}
	if h1 == h2 {
		t.Error("two key hashes should not be equal")
	}
}

func TestGenerateAPIKey_HashMatchesPlaintext(t *testing.T) {
	plaintext, hash, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	computed := HashAPIKey(plaintext)
	if computed != hash {
		t.Errorf("HashAPIKey(plaintext) = %q, want %q", computed, hash)
	}
}

func TestHashAPIKey_Deterministic(t *testing.T) {
	key := "tdm_somekey"
	h1 := HashAPIKey(key)
	h2 := HashAPIKey(key)
	if h1 != h2 {
		t.Error("HashAPIKey should be deterministic")
	}
}

func TestHashAPIKey_DifferentInputs(t *testing.T) {
	h1 := HashAPIKey("tdm_aaaa")
	h2 := HashAPIKey("tdm_bbbb")
	if h1 == h2 {
		t.Error("different keys should produce different hashes")
	}
}
