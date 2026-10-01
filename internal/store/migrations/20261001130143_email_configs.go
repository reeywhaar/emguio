package migrations

// A user's mail accounts. Passwords are sealed with the server key before they get here.
var emailConfigs = Migration{
	Name: "20261001130143_email_configs",
	Up: exec(`
CREATE TABLE email_configs (
  id                TEXT PRIMARY KEY,
  user_id           TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  name              TEXT NOT NULL DEFAULT '',
  email             TEXT NOT NULL,
  incoming_protocol TEXT NOT NULL,
  incoming_host     TEXT NOT NULL,
  incoming_port     INTEGER NOT NULL,
  incoming_tls      TEXT NOT NULL,
  incoming_username TEXT NOT NULL,
  incoming_secret   BLOB NOT NULL,
  -- A NULL host is no outgoing server. A NULL username is one that signs in as the incoming
  -- server does, with no secret of its own.
  outgoing_host     TEXT,
  outgoing_port     INTEGER,
  outgoing_tls      TEXT,
  outgoing_username TEXT,
  outgoing_secret   BLOB,
  created_at        INTEGER NOT NULL,
  updated_at        INTEGER NOT NULL
);
CREATE INDEX email_configs_user ON email_configs (user_id, created_at);
`),
}
