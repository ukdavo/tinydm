package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// BCryptCost is the work factor passed to bcrypt.GenerateFromPassword.
// Tests should lower this to bcrypt.MinCost (4) for speed:
//
//	func TestMain(m *testing.M) { auth.BCryptCost = bcrypt.MinCost; os.Exit(m.Run()) }
var BCryptCost = 12

// HashPassword returns a bcrypt hash of password using BCryptCost.
func HashPassword(password string) (string, error) {
	if len(password) == 0 {
		return "", fmt.Errorf("password must not be empty")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), BCryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(b), nil
}

// CheckPassword returns nil if password matches hash, or bcrypt.ErrMismatchedHashAndPassword otherwise.
func CheckPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
