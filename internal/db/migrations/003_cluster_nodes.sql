-- +goose Up

-- cluster_nodes tracks every running TinyDM instance.
-- Used for heartbeat monitoring and leader election observability.
-- On single-node / SQLite deployments this table exists but is never written to.
CREATE TABLE IF NOT EXISTS cluster_nodes (
    node_id        TEXT    NOT NULL PRIMARY KEY,
    last_heartbeat TEXT    NOT NULL DEFAULT (datetime('now')),
    is_leader      INTEGER NOT NULL DEFAULT 0
);

-- +goose Down

DROP TABLE IF EXISTS cluster_nodes;
