package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"tinydm/internal/db"
)

// User is a row from the users table.
type User struct {
	ID           string
	TenantID     string
	Username     string
	Email        string
	PasswordHash string
	UserType     UserType
	IsActive     bool
}

// APIKey is a row from the api_keys table.
type APIKey struct {
	ID        string
	TenantID  string
	UserID    sql.NullString
	Name      string
	KeyHash   string
	KeyPrefix string
	ExpiresAt sql.NullTime
	RevokedAt sql.NullTime
}

// Right is a row from the rights table.
type Right struct {
	PrincipalType string
	PrincipalID   string
	ResourceType  string
	ResourceID    string
	CanCreate     bool
	CanRead       bool
	CanUpdate     bool
	CanDelete     bool
}

// Store handles all auth-related database operations using raw SQL so that
// the auth package has no dependency on the sqlc-generated code.
type Store struct {
	db *db.DB
}

// NewStore creates a new auth Store backed by db.
func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

// ─── Users ────────────────────────────────────────────────────────────────────

// GetUserByUsername fetches an active user by tenant + username.
func (s *Store) GetUserByUsername(ctx context.Context, tenantID, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, username, email, password_hash, user_type, is_active
		FROM users
		WHERE tenant_id = ? AND username = ? AND deleted_at IS NULL`,
		tenantID, username,
	)
	return scanUser(row)
}

// GetUserByID fetches an active user by primary key.
func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, username, email, password_hash, user_type, is_active
		FROM users
		WHERE id = ? AND deleted_at IS NULL`,
		id,
	)
	return scanUser(row)
}

// CreateUser inserts a new user row. The password must already be hashed.
func (s *Store) CreateUser(ctx context.Context, tenantID, username, email, passwordHash string, userType UserType) (*User, error) {
	id := uuid.New().String()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, tenant_id, username, email, password_hash, user_type)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, tenantID, username, email, passwordHash, string(userType),
	)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return s.GetUserByID(ctx, id)
}

// CountUsers returns the total number of non-deleted users across all tenants.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&n)
	return n, err
}

// ListUsers returns non-deleted users for the given tenant, ordered by username.
// limit/offset control pagination; pass 0 for limit to use the default (50).
// Returns the matched slice, the total count (unpaged), and any error.
func (s *Store) ListUsers(ctx context.Context, tenantID string, limit, offset int) ([]*User, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE tenant_id = ? AND deleted_at IS NULL`, tenantID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, username, email, password_hash, user_type, is_active
		FROM users
		WHERE tenant_id = ? AND deleted_at IS NULL
		ORDER BY username
		LIMIT ? OFFSET ?`,
		tenantID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		var isActive int
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Username, &u.Email,
			&u.PasswordHash, &u.UserType, &isActive); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		u.IsActive = isActive == 1
		users = append(users, &u)
	}
	return users, total, rows.Err()
}

// SetUserActive enables or disables a user account.
func (s *Store) SetUserActive(ctx context.Context, id string, active bool) error {
	val := 0
	if active {
		val = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET is_active = ? WHERE id = ? AND deleted_at IS NULL`,
		val, id,
	)
	return err
}

// DeleteUser soft-deletes a user by ID. Superadmin accounts cannot be deleted.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	u, err := s.GetUserByID(ctx, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if u != nil && u.UserType == UserTypeSuperAdmin {
		return fmt.Errorf("superadmin account cannot be deleted")
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL`,
		id,
	)
	return err
}

// ─── API keys ─────────────────────────────────────────────────────────────────

// GetAPIKeyByHash fetches an API key row by its SHA-256 hash.
// Returns nil, nil when the key does not exist.
func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, user_id, name, key_hash, key_prefix, expires_at, revoked_at
		FROM api_keys
		WHERE key_hash = ?`,
		hash,
	)
	var k APIKey
	if err := row.Scan(&k.ID, &k.TenantID, &k.UserID, &k.Name,
		&k.KeyHash, &k.KeyPrefix, &k.ExpiresAt, &k.RevokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get api key by hash: %w", err)
	}
	return &k, nil
}

// CreateAPIKey inserts a new API key. keyHash and keyPrefix are computed by
// the caller via GenerateAPIKey().
func (s *Store) CreateAPIKey(ctx context.Context, tenantID string, userID *string, name, keyHash, keyPrefix string, expiresAt *time.Time) (*APIKey, error) {
	id := uuid.New().String()
	var uid sql.NullString
	if userID != nil {
		uid = sql.NullString{String: *userID, Valid: true}
	}
	var exp sql.NullTime
	if expiresAt != nil {
		exp = sql.NullTime{Time: *expiresAt, Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_keys (id, tenant_id, user_id, name, key_hash, key_prefix, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, uid, name, keyHash, keyPrefix, exp,
	)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return s.GetAPIKeyByHash(ctx, keyHash)
}

// ListAPIKeys returns API keys for the given tenant, most recently created first.
// limit/offset control pagination; pass 0 for limit to use the default (50).
// Returns the matched slice, the total count (unpaged), and any error.
func (s *Store) ListAPIKeys(ctx context.Context, tenantID string, limit, offset int) ([]*APIKey, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE tenant_id = ?`, tenantID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count api keys: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, user_id, name, key_hash, key_prefix, expires_at, revoked_at
		FROM api_keys
		WHERE tenant_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`,
		tenantID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.TenantID, &k.UserID, &k.Name,
			&k.KeyHash, &k.KeyPrefix, &k.ExpiresAt, &k.RevokedAt); err != nil {
			return nil, 0, fmt.Errorf("scan api key: %w", err)
		}
		keys = append(keys, &k)
	}
	return keys, total, rows.Err()
}

// RevokeAPIKey sets revoked_at to now for the given key ID.
func (s *Store) RevokeAPIKey(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = CURRENT_TIMESTAMP WHERE id = ? AND revoked_at IS NULL`,
		id,
	)
	return err
}

// TouchAPIKey updates last_used_at for a key. Errors are intentionally ignored
// by callers — a failed touch should not block the request.
func (s *Store) TouchAPIKey(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		time.Now().UTC(), id,
	)
	return err
}

// ─── RBAC ─────────────────────────────────────────────────────────────────────

// GetUserRights returns all rights for a user, including rights inherited via
// group membership.
func (s *Store) GetUserRights(ctx context.Context, tenantID, userID string) ([]Right, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.principal_type, r.principal_id, r.resource_type, r.resource_id,
		       r.can_create, r.can_read, r.can_update, r.can_delete
		FROM rights r
		WHERE r.tenant_id = ?
		  AND (
		      (r.principal_type = 'user'  AND r.principal_id = ?)
		   OR (r.principal_type = 'group' AND r.principal_id IN (
		           SELECT group_id FROM group_members WHERE user_id = ?
		       ))
		  )`,
		tenantID, userID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query rights: %w", err)
	}
	defer rows.Close()

	var rights []Right
	for rows.Next() {
		var r Right
		var cc, cr, cu, cd int
		if err := rows.Scan(
			&r.PrincipalType, &r.PrincipalID,
			&r.ResourceType, &r.ResourceID,
			&cc, &cr, &cu, &cd,
		); err != nil {
			return nil, fmt.Errorf("scan right: %w", err)
		}
		r.CanCreate = cc == 1
		r.CanRead = cr == 1
		r.CanUpdate = cu == 1
		r.CanDelete = cd == 1
		rights = append(rights, r)
	}
	return rights, rows.Err()
}

// ─── Bootstrap ────────────────────────────────────────────────────────────────

// EnsureAdminUser creates the global superadmin and a domain admin for the
// bootstrap tenant if no users exist in the DB. It is a no-op on subsequent
// calls. Returns the one-time plaintext password for the domain admin (non-empty
// only on the very first call), or an error.
func (s *Store) EnsureAdminUser(ctx context.Context, tenantID, tenantName, username, email, password string) (string, error) {
	n, err := s.CountUsers(ctx)
	if err != nil {
		return "", fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		return "", nil // already bootstrapped
	}

	// Create the bootstrap tenant if it doesn't exist.
	var exists int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenants WHERE id = ?`, tenantID).Scan(&exists)
	if exists == 0 {
		tid := tenantID
		if tid == "" {
			tid = uuid.New().String()
		}
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO tenants (id, name, description) VALUES (?, ?, ?)`,
			tid, tenantName, "System tenant",
		); err != nil {
			return "", fmt.Errorf("create bootstrap tenant: %w", err)
		}
		tenantID = tid
	}

	hash, err := HashPassword(password)
	if err != nil {
		return "", fmt.Errorf("hash bootstrap password: %w", err)
	}

	// The bootstrap user is a superadmin, not a plain domain admin.
	if _, err := s.CreateUser(ctx, tenantID, username, email, hash, UserTypeSuperAdmin); err != nil {
		return "", fmt.Errorf("create bootstrap superadmin: %w", err)
	}

	// Also provision a domain admin for the bootstrap tenant so the default
	// domain has a scoped admin account from day one.
	_, domainAdminPass, err := s.CreateDomainAdmin(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("create bootstrap domain admin: %w", err)
	}
	return domainAdminPass, nil
}

// CreateDomainAdmin creates a domain admin user for the given tenant and
// returns the new user together with the plaintext password. The plaintext
// password is generated internally using crypto/rand and is never persisted —
// the caller is responsible for conveying it to the operator (e.g. in the
// HTTP response for the tenant creation request).
func (s *Store) CreateDomainAdmin(ctx context.Context, tenantID string) (*User, string, error) {
	plaintext, err := generatePassword(20)
	if err != nil {
		return nil, "", fmt.Errorf("generate domain admin password: %w", err)
	}
	hash, err := HashPassword(plaintext)
	if err != nil {
		return nil, "", fmt.Errorf("hash domain admin password: %w", err)
	}
	user, err := s.CreateUser(ctx, tenantID, "admin", "", hash, UserTypeAdmin)
	if err != nil {
		return nil, "", fmt.Errorf("create domain admin: %w", err)
	}
	return user, plaintext, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// generatePassword returns a URL-safe random password of approximately n
// printable characters, derived from crypto/rand bytes encoded as base64.
func generatePassword(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	// base64 URL encoding without padding gives ~4/3 × n characters; trim to n.
	encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(buf)
	if len(encoded) > n {
		encoded = encoded[:n]
	}
	return encoded, nil
}

func scanUser(row *sql.Row) (*User, error) {
	var u User
	var isActive int
	if err := row.Scan(
		&u.ID, &u.TenantID, &u.Username, &u.Email,
		&u.PasswordHash, &u.UserType, &isActive,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}
	u.IsActive = isActive == 1
	return &u, nil
}
