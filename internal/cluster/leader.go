package cluster

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// leaderLockID is the fixed PostgreSQL advisory lock identifier used for
// cluster leader election. All nodes compete for this single lock; the holder
// is the current leader.
//
// Value: fnv64a("tinydm:leader") = 443198911189067267
const leaderLockID = int64(443198911189067267)

// heartbeatInterval is how often each node updates its last_heartbeat in the
// cluster_nodes table and the leader re-asserts its advisory lock.
const heartbeatInterval = 5 * time.Second

// LeaderElector coordinates cluster leadership. The current leader is the node
// that holds the PostgreSQL session-level advisory lock leaderLockID.
//
// For single-node / SQLite deployments use NewNoOpLeaderElector, which always
// reports itself as the leader without touching the database.
type LeaderElector interface {
	// Start begins the heartbeat loop and attempts to acquire leadership.
	// It must be called before IsLeader.
	Start(ctx context.Context) error
	// Stop gracefully releases the advisory lock and stops the heartbeat.
	Stop()
	// IsLeader reports whether this node currently holds the leader lock.
	IsLeader() bool
	// NodeID returns the stable identifier for this node.
	NodeID() string
}

// ─── NoOpLeaderElector ────────────────────────────────────────────────────────

// NoOpLeaderElector is used for single-node / SQLite deployments. It always
// reports the node as the leader and requires no database coordination.
type NoOpLeaderElector struct {
	nodeID string
}

// NewNoOpLeaderElector returns a LeaderElector that always reports leadership.
func NewNoOpLeaderElector(nodeID string) LeaderElector {
	return &NoOpLeaderElector{nodeID: nodeID}
}

func (e *NoOpLeaderElector) Start(_ context.Context) error { return nil }
func (e *NoOpLeaderElector) Stop()                          {}
func (e *NoOpLeaderElector) IsLeader() bool                 { return true }
func (e *NoOpLeaderElector) NodeID() string                 { return e.nodeID }

// ─── PGLeaderElector ──────────────────────────────────────────────────────────

// PGLeaderElector implements LeaderElector for PostgreSQL multi-node clusters.
// It uses a dedicated connection to hold a session-level advisory lock for
// as long as the process is alive. On startup it tries a non-blocking acquire;
// if successful the node becomes the leader immediately. Other nodes become
// leader when the current leader stops (its connection closes, releasing the
// advisory lock automatically).
type PGLeaderElector struct {
	nodeID string
	db     *sql.DB
	conn   *sql.Conn // dedicated connection that holds the advisory lock

	leader atomic.Bool
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewPGLeaderElector creates a PGLeaderElector. Call Start to begin
// coordination.
func NewPGLeaderElector(nodeID string, db *sql.DB) LeaderElector {
	return &PGLeaderElector{
		nodeID: nodeID,
		db:     db,
		stopCh: make(chan struct{}),
	}
}

// Start acquires a dedicated connection, attempts a non-blocking leader lock,
// records the node in cluster_nodes, and begins the heartbeat goroutine.
func (e *PGLeaderElector) Start(ctx context.Context) error {
	conn, err := e.db.Conn(ctx)
	if err != nil {
		return err
	}
	e.conn = conn

	// Try to become leader immediately (non-blocking).
	var isLeader bool
	if err := conn.QueryRowContext(ctx,
		"SELECT pg_try_advisory_lock($1)", leaderLockID,
	).Scan(&isLeader); err != nil {
		conn.Close() //nolint:errcheck
		return err
	}
	e.leader.Store(isLeader)
	slog.Info("leader election", "node_id", e.nodeID, "is_leader", isLeader)

	// Register this node in the cluster_nodes table.
	e.upsertHeartbeat(ctx)

	e.wg.Add(1)
	go e.heartbeatLoop()
	return nil
}

// Stop releases the advisory lock (if held), updates cluster_nodes, and
// stops the heartbeat goroutine.
func (e *PGLeaderElector) Stop() {
	close(e.stopCh)
	e.wg.Wait()

	if e.conn != nil {
		bg := context.Background()
		if e.leader.Load() {
			e.conn.ExecContext(bg, "SELECT pg_advisory_unlock($1)", leaderLockID) //nolint:errcheck
		}
		// Mark node as non-leader on clean shutdown.
		e.db.ExecContext(bg, //nolint:errcheck
			"UPDATE cluster_nodes SET is_leader = FALSE WHERE node_id = $1", e.nodeID)
		e.conn.Close() //nolint:errcheck
	}
}

func (e *PGLeaderElector) IsLeader() bool { return e.leader.Load() }
func (e *PGLeaderElector) NodeID() string  { return e.nodeID }

// heartbeatLoop runs every heartbeatInterval. It updates last_heartbeat and,
// if this node is not yet the leader, attempts to acquire the advisory lock.
func (e *PGLeaderElector) heartbeatLoop() {
	defer e.wg.Done()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

			e.upsertHeartbeat(ctx)

			// If not yet the leader, try a non-blocking lock acquisition.
			// (Once acquired the session-level lock stays until Stop is called.)
			if !e.leader.Load() {
				var won bool
				if err := e.conn.QueryRowContext(ctx,
					"SELECT pg_try_advisory_lock($1)", leaderLockID,
				).Scan(&won); err == nil && won {
					e.leader.Store(true)
					slog.Info("leader election: this node became leader", "node_id", e.nodeID)
				}
			}

			cancel()
		}
	}
}

// upsertHeartbeat writes or refreshes this node's row in cluster_nodes.
func (e *PGLeaderElector) upsertHeartbeat(ctx context.Context) {
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (node_id, last_heartbeat, is_leader)
		VALUES ($1, NOW(), $2)
		ON CONFLICT (node_id) DO UPDATE
		SET last_heartbeat = EXCLUDED.last_heartbeat,
		    is_leader       = EXCLUDED.is_leader
	`, e.nodeID, e.leader.Load())
	if err != nil {
		slog.Warn("cluster heartbeat failed", "node_id", e.nodeID, "error", err)
	}
}
