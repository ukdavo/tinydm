// Package auth handles authentication, authorisation, and principal management.
package auth

import "context"

type contextKey string

const principalKey contextKey = "principal"

// UserType distinguishes administrator users from regular users.
type UserType string

const (
	UserTypeAdmin UserType = "admin"
	UserTypeUser  UserType = "user"
)

// AuthMethod records how a principal was authenticated.
type AuthMethod string

const (
	AuthMethodBasic  AuthMethod = "basic"
	AuthMethodBearer AuthMethod = "bearer"
	AuthMethodAPIKey AuthMethod = "apikey"
)

// Principal represents an authenticated caller attached to a request context.
type Principal struct {
	ID         string
	TenantID   string
	Username   string
	UserType   UserType
	AuthMethod AuthMethod
}

// IsAdmin reports whether the principal has administrator rights.
func (p Principal) IsAdmin() bool {
	return p.UserType == UserTypeAdmin
}

// WithPrincipal returns a new context carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// PrincipalFromContext retrieves the Principal stored by WithPrincipal.
// Returns the zero value and false if no principal is present.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok
}
