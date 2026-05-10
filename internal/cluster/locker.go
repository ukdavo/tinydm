// Package cluster provides distributed coordination primitives for TinyDM
// running in multi-node configurations. All types degrade gracefully to
// no-op implementations when running on a single node with SQLite.
package cluster

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
)

// Locker acquires a distributed advisory lock identified by an arbitrary
// string key. Implementations must be safe for concurrent use.
//
// Lock blocks until the lock is acquired or ctx is cancelled. The returned
// unlock function releases the lock and must always be called (defer it).
//
// For single-node deployments the NoOpLocker returns immediately and the
// unlock function is a no-op. For multi-node deployments the PGLocker
// serialises concurrent writers across all nodes using PostgreSQL session-level
// advisory locks.
type Locker interface {
	Lock(ctx context.Context, key string) (unlock func(), err error)
}

// ─── NoOpLocker ───────────────────────────────────────────────────────────────

// NoOpLocker is the Locker implementation used for single-node / SQLite
// deployments. It acquires nothing and returns a no-op unlock function.
// Because SQLite serialises all writes through a single connection, no
// additional distributed locking is required.
type NoOpLocker struct{}

// NewNoOpLocker returns a NoOpLocker.
func NewNoOpLocker() *NoOpLocker { return &NoOpLocker{} }

// Lock returns immediately with a no-op unlock function.
func (n *NoOpLocker) Lock(_ context.Context, _ string) (func(), error) {
	return func() {}, nil
}

// ─── PGLocker ─────────────────────────────────────────────────────────────────

// PGLocker is the Locker implementation for PostgreSQL multi-node
// deployments. It uses PostgreSQL session-level advisory locks
// (pg_advisory_lock / pg_advisory_unlock) to provide cluster-wide mutual
// exclusion.
//
// Each call to Lock acquires a dedicated connection from the pool, runs
// pg_advisory_lock on it, and returns an unlock function that runs
// pg_advisory_unlock and returns the connection to the pool. Because the lock
// is session-level rather than transaction-level, it remains held across
// multiple DB operations within the same handler.
type PGLocker struct {
	db *sql.DB
}

// NewPGLocker creates a PGLocker backed by db.
// db must be connected to a PostgreSQL instance.
func NewPGLocker(db *sql.DB) *PGLocker {
	return &PGLocker{db: db}
}

// Lock acquires a PostgreSQL session-level advisory lock for key. The lock
// blocks until it is acquired or ctx is cancelled. The returned function
// releases the lock; it must always be called.
func (l *PGLocker) Lock(ctx context.Context, key string) (func(), error) {
	lockID := keyToInt64(key)

	// Each lock gets its own dedicated connection so that the advisory lock
	// is tied to a single session and cannot be accidentally released by
	// another goroutine sharing the same connection.
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg lock %q: acquire connection: %w", key, err)
	}

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		conn.Close() //nolint:errcheck
		return nil, fmt.Errorf("pg lock %q: %w", key, err)
	}

	unlock := func() {
		// Use a fresh background context — the request context may already be
		// cancelled when the handler returns.
		conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", lockID) //nolint:errcheck
		conn.Close()                                                                     //nolint:errcheck
	}
	return unlock, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// keyToInt64 converts an arbitrary string key to a stable int64 advisory lock
// identifier using the FNV-1a 64-bit hash. The same function is used on every
// node, so lock IDs are consistent across the cluster.
func keyToInt64(key string) int64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	return int64(h.Sum64())
}
