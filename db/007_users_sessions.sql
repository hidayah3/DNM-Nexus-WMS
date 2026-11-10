-- Staff accounts and sign-in sessions for the WMS sign-in page.
-- Passwords are stored only as bcrypt hashes. Sessions store a SHA-256 of the
-- cookie token, never the token itself.
BEGIN;

CREATE TABLE IF NOT EXISTS wms_user (
    username      text PRIMARY KEY,
    display_name  text NOT NULL,
    password_hash text NOT NULL,
    role          text NOT NULL DEFAULT 'warehouse_admin' CHECK (role IN ('admin','warehouse_admin')),
    active        boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz
);
-- Usernames are matched without regard to capitals ("Admin" = "admin").
CREATE UNIQUE INDEX IF NOT EXISTS wms_user_lower_idx ON wms_user (lower(username));

CREATE TABLE IF NOT EXISTS wms_session (
    token_hash text PRIMARY KEY,
    username   text NOT NULL REFERENCES wms_user(username) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS wms_session_expires_idx ON wms_session (expires_at);

COMMIT;
