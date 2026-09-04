-- +migrate Up
CREATE TABLE failback_intents (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    backend_id TEXT NOT NULL REFERENCES backends(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_failback_node ON failback_intents(node_id);
