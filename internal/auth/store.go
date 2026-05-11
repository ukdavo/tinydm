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
	Username     string
	Email        string
	FirstName    string
	LastName     string
	PasswordHash string
	UserType     UserType
	IsActive     bool
}

// APIKey is a row from the api_keys table.
type APIKey struct {
	ID        string
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

// GetUserByUsername fetches an active user by username.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, email, first_name, last_name, password_hash, user_type, is_active
		FROM users
		WHERE username = ? AND deleted_at IS NULL`,
		username,
	)
	return scanUser(row)
}

// GetUserByID fetches an active user by primary key.
func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, email, first_name, last_name, password_hash, user_type, is_active
		FROM users
		WHERE id = ? AND deleted_at IS NULL`,
		id,
	)
	return scanUser(row)
}

// GetUserByEmail fetches an active user by email address.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, email, first_name, last_name, password_hash, user_type, is_active
		FROM users
		WHERE email = ? AND deleted_at IS NULL`,
		email,
	)
	return scanUser(row)
}

// CreateUser inserts a new user row. The password must already be hashed.
func (s *Store) CreateUser(ctx context.Context, username, email, firstName, lastName, passwordHash string, userType UserType) (*User, error) {
	id := uuid.New().String()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, email, first_name, last_name, password_hash, user_type)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, username, email, firstName, lastName, passwordHash, string(userType),
	)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return s.GetUserByID(ctx, id)
}

// ChangePassword updates the password hash for an existing, non-deleted user.
// Returns an error if the user does not exist or has been soft-deleted.
func (s *Store) ChangePassword(ctx context.Context, userID, newHash string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE users
		SET password_hash = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND deleted_at IS NULL`,
		newHash, userID,
	)
	if err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// CountUsers returns the total number of non-deleted users.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&n)
	return n, err
}

// ListUsers returns non-deleted users, ordered by username.
// limit/offset control pagination; pass 0 for limit to use the default (50).
// Returns the matched slice, the total count (unpaged), and any error.
func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]*User, int, error) {
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
		`SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, email, first_name, last_name, password_hash, user_type, is_active
		FROM users
		WHERE deleted_at IS NULL
		ORDER BY username
		LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		var isActive int
		if err := rows.Scan(&u.ID, &u.Username, &u.Email,
			&u.FirstName, &u.LastName,
			&u.PasswordHash, &u.UserType, &isActive); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		u.IsActive = isActive == 1
		users = append(users, &u)
	}
	return users, total, rows.Err()
}

// SetUserActive enables or disables a user account. Admin accounts cannot be
// deactivated.
func (s *Store) SetUserActive(ctx context.Context, id string, active bool) error {
	if !active {
		u, err := s.GetUserByID(ctx, id)
		if err != nil {
			return fmt.Errorf("set active: %w", err)
		}
		if u != nil && u.UserType == UserTypeAdmin {
			return fmt.Errorf("admin accounts cannot be deactivated")
		}
	}
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

// DeleteUser soft-deletes a user by ID. Admin accounts cannot be deleted.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	u, err := s.GetUserByID(ctx, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if u != nil && u.UserType == UserTypeAdmin {
		return fmt.Errorf("admin account cannot be deleted")
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
		SELECT id, user_id, name, key_hash, key_prefix, expires_at, revoked_at
		FROM api_keys
		WHERE key_hash = ?`,
		hash,
	)
	var k APIKey
	if err := row.Scan(&k.ID, &k.UserID, &k.Name,
		&k.KeyHash, &k.KeyPrefix, &k.ExpiresAt, &k.RevokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get api key by hash: %w", err)
	}
	return &k, nil
}

// GetAPIKeyByID fetches an API key row by its primary key.
// Returns nil, nil when the key does not exist.
func (s *Store) GetAPIKeyByID(ctx context.Context, id string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, key_hash, key_prefix, expires_at, revoked_at
		FROM api_keys
		WHERE id = ?`,
		id,
	)
	var k APIKey
	if err := row.Scan(&k.ID, &k.UserID, &k.Name,
		&k.KeyHash, &k.KeyPrefix, &k.ExpiresAt, &k.RevokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get api key by id: %w", err)
	}
	return &k, nil
}

// CreateAPIKey inserts a new API key. keyHash and keyPrefix are computed by
// the caller via GenerateAPIKey().
func (s *Store) CreateAPIKey(ctx context.Context, userID *string, name, keyHash, keyPrefix string, expiresAt *time.Time) (*APIKey, error) {
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
		INSERT INTO api_keys (id, user_id, name, key_hash, key_prefix, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, uid, name, keyHash, keyPrefix, exp,
	)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return s.GetAPIKeyByHash(ctx, keyHash)
}

// ListAPIKeys returns API keys, most recently created first.
// limit/offset control pagination; pass 0 for limit to use the default (50).
// Returns the matched slice, the total count (unpaged), and any error.
func (s *Store) ListAPIKeys(ctx context.Context, limit, offset int) ([]*APIKey, int, error) {
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
		`SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL`,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count api keys: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, name, key_hash, key_prefix, expires_at, revoked_at
		FROM api_keys
		WHERE revoked_at IS NULL
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name,
			&k.KeyHash, &k.KeyPrefix, &k.ExpiresAt, &k.RevokedAt); err != nil {
			return nil, 0, fmt.Errorf("scan api key: %w", err)
		}
		keys = append(keys, &k)
	}
	return keys, total, rows.Err()
}

// RevokeAPIKey sets revoked_at to now for the given key ID.
func (s *Store) RevokeAPIKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND revoked_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("api key not found or already revoked")
	}
	return nil
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
func (s *Store) GetUserRights(ctx context.Context, userID string) ([]Right, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.principal_type, r.principal_id, r.resource_type, r.resource_id,
		       r.can_create, r.can_read, r.can_update, r.can_delete
		FROM rights r
		WHERE (
		    (r.principal_type = 'user'  AND r.principal_id = ?)
		 OR (r.principal_type = 'group' AND r.principal_id IN (
		         SELECT group_id FROM group_members WHERE user_id = ?
		     ))
		)`,
		userID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query rights: %w", err)
	}
	defer rows.Close()
	return scanRights(rows)
}

// GetAPIKeyRights returns all rights for an API key principal.
func (s *Store) GetAPIKeyRights(ctx context.Context, apiKeyID string) ([]Right, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT principal_type, principal_id, resource_type, resource_id,
		       can_create, can_read, can_update, can_delete
		FROM rights
		WHERE principal_type = 'apikey' AND principal_id = ?`,
		apiKeyID,
	)
	if err != nil {
		return nil, fmt.Errorf("get api key rights: %w", err)
	}
	defer rows.Close()
	return scanRights(rows)
}

// UpsertRightParams holds the fields for creating or updating a right row.
type UpsertRightParams struct {
	PrincipalType string // "user" | "group" | "apikey"
	PrincipalID   string
	ResourceType  string // "project" | "bucket" | "document"
	ResourceID    string // specific UUID or "*"
	CanCreate     bool
	CanRead       bool
	CanUpdate     bool
	CanDelete     bool
}

// UpsertRight creates or replaces a right row for the given principal/resource.
func (s *Store) UpsertRight(ctx context.Context, p UpsertRightParams) error {
	id := uuid.New().String()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rights
		    (id, principal_type, principal_id, resource_type, resource_id,
		     can_create, can_read, can_update, can_delete)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(principal_type, principal_id, resource_type, resource_id)
		DO UPDATE SET
		    can_create = excluded.can_create,
		    can_read   = excluded.can_read,
		    can_update = excluded.can_update,
		    can_delete = excluded.can_delete`,
		id, p.PrincipalType, p.PrincipalID,
		p.ResourceType, p.ResourceID,
		boolToInt(p.CanCreate), boolToInt(p.CanRead),
		boolToInt(p.CanUpdate), boolToInt(p.CanDelete),
	)
	if err != nil {
		return fmt.Errorf("upsert right: %w", err)
	}
	return nil
}

// DeleteRight removes the specific right row. Returns an error if the row does
// not exist.
func (s *Store) DeleteRight(ctx context.Context, principalType, principalID, resourceType, resourceID string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM rights
		WHERE principal_type = ? AND principal_id = ?
		  AND resource_type = ? AND resource_id = ?`,
		principalType, principalID, resourceType, resourceID,
	)
	if err != nil {
		return fmt.Errorf("delete right: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("right not found")
	}
	return nil
}

// ListRights returns all rights for a given principal, ordered by resource_type,
// resource_id.
func (s *Store) ListRights(ctx context.Context, principalType, principalID string) ([]Right, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT principal_type, principal_id, resource_type, resource_id,
		       can_create, can_read, can_update, can_delete
		FROM rights
		WHERE principal_type = ? AND principal_id = ?
		ORDER BY resource_type, resource_id`,
		principalType, principalID,
	)
	if err != nil {
		return nil, fmt.Errorf("list rights: %w", err)
	}
	defer rows.Close()
	return scanRights(rows)
}

// ListRightsByResource returns all rights for a given resource, ordered by
// principal_type, principal_id.
func (s *Store) ListRightsByResource(ctx context.Context, resourceType, resourceID string) ([]Right, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT principal_type, principal_id, resource_type, resource_id,
		       can_create, can_read, can_update, can_delete
		FROM rights
		WHERE resource_type = ? AND resource_id = ?
		ORDER BY principal_type, principal_id`,
		resourceType, resourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list rights by resource: %w", err)
	}
	defer rows.Close()
	return scanRights(rows)
}

// ─── Bootstrap ────────────────────────────────────────────────────────────────

// EnsureAdminUser creates the admin user if no users exist in the DB.
// It is a no-op on subsequent calls.
func (s *Store) EnsureAdminUser(ctx context.Context, username, email, password string) error {
	n, err := s.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		return nil // already bootstrapped
	}
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}
	if _, err := s.CreateUser(ctx, username, email, "Admin", "", hash, UserTypeAdmin); err != nil {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}
	return nil
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

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func scanRights(rows *sql.Rows) ([]Right, error) {
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

func scanUser(row interface{ Scan(...interface{}) error }) (*User, error) {
	var u User
	var isActive int
	err := row.Scan(&u.ID, &u.Username, &u.Email,
		&u.FirstName, &u.LastName,
		&u.PasswordHash, &u.UserType, &isActive)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.IsActive = isActive == 1
	return &u, nil
}
