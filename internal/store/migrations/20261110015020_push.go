package migrations

// Notifications: the instance's push keypair, the browsers each user has let notify them, how each
// email config and folder notifies, and the mail that arrived for them to be told about.
var push = Migration{
	Name: "20261110015020_push",
	Up: exec(`
-- One row, so a second keypair is a refused insert rather than a question of which is live.
-- Every subscription is bound to it: losing it ends them all.
CREATE TABLE vapid (
  singleton   INTEGER PRIMARY KEY CHECK (singleton = 1),
  private_key BLOB NOT NULL,
  public_key  BLOB NOT NULL,
  created_at  INTEGER NOT NULL
);

-- An endpoint is unique across users: one browser profile has one subscription, and it is
-- whoever registered it last's.
CREATE TABLE devices (
  id            TEXT PRIMARY KEY,
  user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  endpoint      TEXT NOT NULL UNIQUE,
  p256dh        TEXT NOT NULL,
  auth          TEXT NOT NULL,
  label         TEXT NOT NULL DEFAULT '',
  -- The browser's own id, so a subscription it replaced is replaced here too.
  client_id     TEXT NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL,
  last_ok_at    INTEGER,
  failure_count INTEGER NOT NULL DEFAULT 0,
  last_error    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX devices_user ON devices (user_id, client_id);

-- now, digest or off; a digest every so many minutes.
ALTER TABLE email_configs ADD COLUMN notify TEXT NOT NULL DEFAULT 'now';
ALTER TABLE email_configs ADD COLUMN digest_every INTEGER NOT NULL DEFAULT 60;
-- Null is what the folder is for deciding: INBOX and the user's own notify.
ALTER TABLE mailboxes ADD COLUMN notify INTEGER;

-- Mail that arrived, until it is told and a while after, so the same message filed in a second
-- folder is not told twice. What a notification says is copied: a digest can outlast the
-- message's place in the kept window.
CREATE TABLE arrivals (
  id              INTEGER PRIMARY KEY,
  email_config_id TEXT NOT NULL REFERENCES email_configs (id) ON DELETE CASCADE,
  mailbox_id      TEXT NOT NULL REFERENCES mailboxes (id) ON DELETE CASCADE,
  uidvalidity     INTEGER NOT NULL,
  uid             INTEGER NOT NULL,
  message_id      TEXT NOT NULL DEFAULT '',
  from_name       TEXT NOT NULL DEFAULT '',
  from_email      TEXT NOT NULL DEFAULT '',
  subject         TEXT NOT NULL DEFAULT '',
  preview         TEXT NOT NULL DEFAULT '',
  arrived_at      INTEGER NOT NULL,
  told_at         INTEGER,
  UNIQUE (mailbox_id, uidvalidity, uid)
);
CREATE INDEX arrivals_untold ON arrivals (email_config_id) WHERE told_at IS NULL;
CREATE INDEX arrivals_message ON arrivals (email_config_id, message_id);
`),
}
