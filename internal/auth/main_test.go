package auth

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestMain drops the bcrypt work factor to the minimum so that tests run in
// milliseconds rather than seconds. Production code keeps BCryptCost = 12.
func TestMain(m *testing.M) {
	BCryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}
