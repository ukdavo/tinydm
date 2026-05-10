-- +goose Up

-- cluster_nodes tracks every running TinyDM instance.
-- Used for heartbeat monitoring and leader election observability.
CREATE TABLE IF NOT EXISTS cluster_nodes (
    node_id        TEXT        NOT NULL PRIMARY KEY,
    last_heartbeat TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    is_leader      BOOLEAN     NOT NULL DEFAULT FALSE
);

COMMENT ON TABLE cluster_nodes IS 'One row per live TinyDM node; updated every 5 s via heartbeat.';
COMMENT ON COLUMN cluster_nodes.node_id        IS 'Stable identifier — set via TINYDM_NODE_ID, defaults to hostname.';
COMMENT ON COLUMN cluster_nodes.last_heartbeat IS 'Wall-clock time of the last successful heartbeat from this node.';
COMMENT ON COLUMN cluster_nodes.is_leader      IS 'TRUE for the node currently holding the advisory leader lock.';

-- +goose Down

DROP TABLE IF EXISTS cluster_nodes;
