-- Disabled mappings stay in the catalog but are omitted from desired-state
-- so the agent plans DELETE on the next reconcile. No agent/code-path change.
ALTER TABLE port_mappings ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;
