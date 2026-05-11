package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Authenticator is HTTP middleware that identifies the caller and stores a
// Principal in the request context. The handler is still invoked on failure —
// use RequireAuth or RequireAdmin downstream to enforce access.
//
// Authentication schemes (evaluated in order):
//  1. Authorization: Bearer <JWT>
//  2. Authorization: Basic <base64(username:password)>
//  3. X-API-Key: <key>
func Authenticator(secret string, store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := identify(r, secret, store)
			if err != nil {
				slog.Debug("authentication failed", "error", err, "path", r.URL.Path)
			}
			if p != nil {
				r = r.WithContext(WithPrincipal(r.Context(), *p))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth rejects unauthenticated requests with HTTP 401.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := PrincipalFromContext(r.Context()); !ok {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin rejects non-admin principals with HTTP 403.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}
		if !p.IsAdmin() {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"admin access required"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─── internal helpers ─────────────────────────────────────────────────────────

func identify(r *http.Request, secret string, store *Store) (*Principal, error) {
	authHeader := r.Header.Get("Authorization")

	// 1. Bearer JWT
	if strings.HasPrefix(authHeader, "Bearer ") {
		return verifyJWT(r.Context(), secret, strings.TrimPrefix(authHeader, "Bearer "))
	}

	// 2. Basic auth
	if strings.HasPrefix(authHeader, "Basic ") {
		return verifyBasic(r.Context(), strings.TrimPrefix(authHeader, "Basic "), store)
	}

	// 3. API key
	if key := r.Header.Get("X-API-Key"); key != "" {
		return verifyAPIKey(r.Context(), key, store)
	}

	return nil, nil
}

func verifyJWT(_ context.Context, secret, tokenStr string) (*Principal, error) {
	claims, err := ParseJWT(secret, tokenStr)
	if err != nil {
		return nil, fmt.Errorf("jwt: %w", err)
	}
	return &Principal{
		ID:         claims.Subject,
		Username:   claims.Username,
		UserType:   UserType(claims.UserType),
		AuthMethod: AuthMethodBearer,
	}, nil
}

func verifyBasic(ctx context.Context, encoded string, store *Store) (*Principal, error) {
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("basic: decode: %w", err)
	}
	parts := strings.SplitN(string(b), ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("basic: expected username:password")
	}
	username, password := parts[0], parts[1]

	user, err := store.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("basic: lookup user: %w", err)
	}
	if user == nil || !user.IsActive {
		return nil, fmt.Errorf("basic: user not found or inactive")
	}
	if err := CheckPassword(user.PasswordHash, password); err != nil {
		return nil, fmt.Errorf("basic: invalid password")
	}
	return &Principal{
		ID:         user.ID,
		Username:   user.Username,
		UserType:   user.UserType,
		AuthMethod: AuthMethodBasic,
	}, nil
}

func verifyAPIKey(ctx context.Context, key string, store *Store) (*Principal, error) {
	hash := HashAPIKey(key)

	apiKey, err := store.GetAPIKeyByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("apikey: lookup: %w", err)
	}
	if apiKey == nil {
		return nil, fmt.Errorf("apikey: not found")
	}
	if apiKey.RevokedAt.Valid {
		return nil, fmt.Errorf("apikey: revoked")
	}
	if apiKey.ExpiresAt.Valid && time.Now().After(apiKey.ExpiresAt.Time) {
		return nil, fmt.Errorf("apikey: expired")
	}

	// Best-effort timestamp update — do not block on failure.
	go func() { _ = store.TouchAPIKey(context.Background(), apiKey.ID) }()

	// Key tied to a specific user → use their profile.
	if apiKey.UserID.Valid {
		user, err := store.GetUserByID(ctx, apiKey.UserID.String)
		if err == nil && user != nil && user.IsActive {
			return &Principal{
				ID:         user.ID,
				Username:   user.Username,
				UserType:   user.UserType,
				AuthMethod: AuthMethodAPIKey,
			}, nil
		}
	}

	// Key with no user → treated as admin.
	return &Principal{
		ID:         apiKey.ID,
		Username:   apiKey.Name,
		UserType:   UserTypeAdmin,
		AuthMethod: AuthMethodAPIKey,
	}, nil
}
