-- Add management inventory without replacing Backend or changing desired state.
CREATE TABLE foreign_nodes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    public_address TEXT NOT NULL,
    management_address TEXT NOT NULL DEFAULT '',
    country TEXT NOT NULL DEFAULT '',
    overlay_addresses_json TEXT NOT NULL DEFAULT '[]',
    provider_type TEXT NOT NULL DEFAULT 'UNMANAGED' CHECK(provider_type IN ('UNMANAGED','3X_UI','SHARX')),
    labels_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
