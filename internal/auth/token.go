package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims are the custom JWT claims used by TinyDM.
type Claims struct {
	TenantID string `json:"tid"`
	Username string `json:"usr"`
	UserType string `json:"typ"`
	jwt.RegisteredClaims
}

// NewJWT creates a signed HS256 JWT for the given user.
func NewJWT(secret string, expiryMinutes int, userID, tenantID, username string, userType UserType) (string, error) {
	claims := Claims{
		TenantID: tenantID,
		Username: username,
		UserType: string(userType),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expiryMinutes) * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// ParseJWT validates tokenStr and returns its claims.
func ParseJWT(secret, tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

// GenerateAPIKey creates a cryptographically random API key.
// Returns:
//   - plaintext: the key shown once to the user  ("tdm_<64 hex chars>")
//   - hash:      SHA-256 hex digest stored in the DB
//   - prefix:    first 12 chars for display ("tdm_xxxxxxxx")
func GenerateAPIKey() (plaintext, hash, prefix string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("generate api key bytes: %w", err)
	}
	raw := hex.EncodeToString(b) // 64 hex chars
	plaintext = "tdm_" + raw
	h := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(h[:])
	prefix = plaintext[:12] // "tdm_" + 8 hex chars
	return plaintext, hash, prefix, nil
}

// HashAPIKey returns the SHA-256 hex digest of key, used for lookup.
func HashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
