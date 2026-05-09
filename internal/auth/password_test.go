package auth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPassword(t *testing.T) {
	tests := []struct {
		name      string
		password  string
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "valid password",
			password: "correct-horse-battery-staple",
		},
		{
			name:     "single character",
			password: "x",
		},
		{
			name:     "password with special characters",
			password: "P@$$w0rd!#%^&*()",
		},
		{
			name:      "empty password returns error",
			password:  "",
			wantErr:   true,
			errSubstr: "must not be empty",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := HashPassword(tc.password)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hash == "" {
				t.Fatal("expected non-empty hash")
			}
			if hash == tc.password {
				t.Error("hash must not equal plaintext")
			}
			// Verify the hash is valid bcrypt.
			if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(tc.password)); err != nil {
				t.Errorf("produced hash does not verify: %v", err)
			}
		})
	}
}

func TestHashPassword_Uniqueness(t *testing.T) {
	// bcrypt salts every hash; two calls with the same password must differ.
	h1, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	h2, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if h1 == h2 {
		t.Error("two hashes of the same password should not be equal (bcrypt uses random salt)")
	}
}

func TestCheckPassword(t *testing.T) {
	password := "hunter2"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("setup: HashPassword: %v", err)
	}

	tests := []struct {
		name    string
		hash    string
		pass    string
		wantErr bool
	}{
		{
			name:    "correct password",
			hash:    hash,
			pass:    password,
			wantErr: false,
		},
		{
			name:    "wrong password",
			hash:    hash,
			pass:    "wrong-password",
			wantErr: true,
		},
		{
			name:    "empty password against real hash",
			hash:    hash,
			pass:    "",
			wantErr: true,
		},
		{
			name:    "empty hash",
			hash:    "",
			pass:    password,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckPassword(tc.hash, tc.pass)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
