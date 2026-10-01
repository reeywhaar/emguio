package migrations

// Users and how they get in. Mail arrives in migrations of its own.
var initialSchema = Migration{
	Name: "20261001122637_initial_schema",
	Up: exec(`
CREATE TABLE users (
  id            TEXT PRIMARY KEY,
  username      TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  created_at    INTEGER NOT NULL
);
-- Rather than a lower() at every call site, so "Misha" and "misha" cannot both register.
CREATE UNIQUE INDEX users_username ON users (lower(username));

CREATE TABLE invites (
  id          TEXT PRIMARY KEY,
  token_hash  BLOB NOT NULL UNIQUE,
  created_at  INTEGER NOT NULL,
  expires_at  INTEGER NOT NULL,
  accepted_at INTEGER,
  user_id     TEXT
);

CREATE TABLE sessions (
  id_hash      BLOB PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  user_agent   TEXT NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions (user_id, last_seen_at DESC);
CREATE INDEX sessions_expiry ON sessions (expires_at);
`),
}
