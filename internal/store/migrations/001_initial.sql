-- +migrate Up
PRAGMA foreign_keys = ON;

CREATE TABLE nodes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    public_ip TEXT NOT NULL DEFAULT '',
    labels_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE backends (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    address TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE tunnels (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    backend_id TEXT NOT NULL REFERENCES backends(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK(type IN ('WIREGUARD', 'SSH_TUN')),
    interface_name TEXT NOT NULL,
    local_overlay_ip TEXT NOT NULL,
    remote_overlay_ip TEXT NOT NULL,
    listen_port INTEGER NOT NULL DEFAULT 0,
    endpoint TEXT NOT NULL DEFAULT '',
    allowed_ips_json TEXT NOT NULL DEFAULT '[]',
    persistent_keepalive INTEGER NOT NULL DEFAULT 0,
    priority INTEGER NOT NULL DEFAULT 0,
    private_key_path TEXT NOT NULL DEFAULT '',
    public_key TEXT NOT NULL DEFAULT '',
    service_name TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE port_mappings (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    backend_id TEXT NOT NULL REFERENCES backends(id) ON DELETE CASCADE,
    protocol TEXT NOT NULL CHECK(protocol IN ('TCP', 'UDP')),
    public_port INTEGER NOT NULL,
    backend_port INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(node_id, protocol, public_port)
);

CREATE TABLE sni_routes (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    listen TEXT NOT NULL,
    matches_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE health_checks (
    id TEXT PRIMARY KEY,
    backend_id TEXT NOT NULL REFERENCES backends(id) ON DELETE CASCADE,
    protocol TEXT NOT NULL,
    port INTEGER NOT NULL,
    interval_ms INTEGER NOT NULL DEFAULT 10000,
    timeout_ms INTEGER NOT NULL DEFAULT 2000,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE failover_policies (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    backend_id TEXT NOT NULL REFERENCES backends(id) ON DELETE CASCADE,
    automatic_failback INTEGER NOT NULL DEFAULT 1,
    automatic_failforward INTEGER NOT NULL DEFAULT 0,
    check_interval_ms INTEGER NOT NULL DEFAULT 10000,
    failure_threshold INTEGER NOT NULL DEFAULT 3,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(node_id, backend_id)
);

CREATE TABLE agent_status (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    healthy INTEGER NOT NULL DEFAULT 0,
    last_reconcile TEXT,
    last_heartbeat TEXT,
    version TEXT NOT NULL DEFAULT '',
    actual_state_json TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL
);

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    timestamp TEXT NOT NULL,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    resource TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '',
    success INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE tokens (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('operator', 'readonly', 'agent')),
    created_at TEXT NOT NULL,
    revoked INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_backends_node ON backends(node_id);
CREATE INDEX idx_tunnels_node ON tunnels(node_id);
CREATE INDEX idx_mappings_node ON port_mappings(node_id);
CREATE INDEX idx_audit_ts ON audit_events(timestamp);
