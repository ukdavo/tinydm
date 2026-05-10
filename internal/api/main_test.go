package api_test

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"tinydm/internal/auth"
)

// TestMain drops the bcrypt work factor to the minimum so that the many
// seedAdminUser calls in integration tests don't blow the test timeout.
// Production code keeps auth.BCryptCost = 12.
func TestMain(m *testing.M) {
	auth.BCryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}
