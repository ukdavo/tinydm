// Package auth handles authentication, authorisation, and principal management.
package auth

import "context"

type contextKey string

const principalKey contextKey = "principal"

// UserType distinguishes the three tiers of access.
type UserType string

const (
	// UserTypeSuperAdmin can create and manage domains (tenants). There is
	// exactly one superadmin created at bootstrap; it is not scoped to any
	// tenant and has unrestricted access to the entire deployment.
	UserTypeSuperAdmin UserType = "superadmin"
	// UserTypeAdmin (domain admin) has full control within a single tenant.
	// One admin is automatically created when a new tenant is provisioned.
	UserTypeAdmin UserType = "admin"
	// UserTypeUser can manage documents within their tenant.
	UserTypeUser UserType = "user"
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

// IsSuperAdmin reports whether the principal is the global superadmin.
// Superadmins are not scoped to a single tenant and can manage all domains.
func (p Principal) IsSuperAdmin() bool {
	return p.UserType == UserTypeSuperAdmin
}

// IsAdmin reports whether the principal has administrator rights within their
// tenant. Both domain admins and superadmins satisfy this check — superadmin
// is a strict superset of admin.
func (p Principal) IsAdmin() bool {
	return p.UserType == UserTypeAdmin || p.UserType == UserTypeSuperAdmin
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
