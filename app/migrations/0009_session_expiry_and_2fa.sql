-- Configurable session expiry (per user) + TOTP two-factor authentication.
--
-- users.session_expiry_days: session lifetime in days; NULL means "never
-- expire". Existing users get the 30-day default.
-- users.totp_secret / totp_enabled / recovery_codes: Google Authenticator
-- enrollment. recovery_codes is a JSON array of SHA-256 hex digests of the
-- single-use codes (plaintext is shown exactly once, at activation).
--
-- sessions.expires_at becomes nullable: a NULL row never expires. The table
-- is rebuilt because SQLite cannot drop a NOT NULL constraint in place.
-- sessions.verified_at records the last successful step-up re-auth; step-up
-- is required for password change, 2FA changes and data export.
--
-- pending_logins holds a half-login while a user with 2FA enabled enters
-- their code: password verified, session not yet started.

ALTER TABLE users ADD COLUMN session_expiry_days INTEGER;
UPDATE users SET session_expiry_days = 30 WHERE session_expiry_days IS NULL;

ALTER TABLE users ADD COLUMN totp_secret TEXT;
ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN recovery_codes TEXT;

CREATE TABLE sessions_new (
    token       TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    expires_at  TEXT,
    verified_at TEXT
);
INSERT INTO sessions_new (token, user_id, created_at, expires_at)
    SELECT token, user_id, created_at, expires_at FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_new RENAME TO sessions;
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE pending_logins (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    expires_at TEXT NOT NULL
);
CREATE INDEX idx_pending_logins_user ON pending_logins(user_id);
