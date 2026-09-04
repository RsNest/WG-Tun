-- Human Web UI accounts. API tokens remain in the tokens table.
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('administrator', 'operator', 'readonly')),
    locale TEXT NOT NULL DEFAULT 'en',
    totp_secret TEXT NOT NULL DEFAULT '',
    totp_pending TEXT NOT NULL DEFAULT '',
    totp_confirmed INTEGER NOT NULL DEFAULT 0,
    disabled INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_login_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_recovery_codes (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    used_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX user_recovery_codes_user ON user_recovery_codes(user_id);
