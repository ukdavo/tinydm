package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// BenchmarkHashPassword_DefaultCost reflects production behaviour (cost 12).
// Expect ~100–300 ms per op depending on hardware.
func BenchmarkHashPassword_DefaultCost(b *testing.B) {
	orig := BCryptCost
	BCryptCost = bcrypt.DefaultCost
	defer func() { BCryptCost = orig }()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := HashPassword("correct-horse-battery-staple"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHashPassword_MinCost isolates bcrypt CPU cost by using cost 4.
func BenchmarkHashPassword_MinCost(b *testing.B) {
	orig := BCryptCost
	BCryptCost = bcrypt.MinCost
	defer func() { BCryptCost = orig }()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := HashPassword("correct-horse-battery-staple"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCheckPassword measures bcrypt comparison time.
// Uses a MinCost hash so the benchmark is fast enough to collect meaningful samples.
func BenchmarkCheckPassword(b *testing.B) {
	orig := BCryptCost
	BCryptCost = bcrypt.MinCost
	hash, err := HashPassword("correct-horse-battery-staple")
	BCryptCost = orig
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CheckPassword(hash, "correct-horse-battery-staple")
	}
}

// BenchmarkNewJWT measures JWT creation (HMAC-SHA256 signing).
func BenchmarkNewJWT(b *testing.B) {
	secret := "bench-secret-32-bytes-long-xxxx"

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := NewJWT(secret, 60, "user-uuid-1234", "alice", UserTypeAdmin); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseJWT measures JWT validation (HMAC-SHA256 verification + claims decode).
func BenchmarkParseJWT(b *testing.B) {
	secret := "bench-secret-32-bytes-long-xxxx"
	token, err := NewJWT(secret, 60, "user-uuid-1234", "alice", UserTypeAdmin)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParseJWT(secret, token); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGenerateAPIKey measures random key generation + SHA-256 hashing.
func BenchmarkGenerateAPIKey(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := GenerateAPIKey(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHashAPIKey measures SHA-256 hashing of an opaque API key string.
// This runs on every authenticated request that uses an API key.
func BenchmarkHashAPIKey(b *testing.B) {
	_, raw, _, err := GenerateAPIKey()
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HashAPIKey(raw)
	}
}
