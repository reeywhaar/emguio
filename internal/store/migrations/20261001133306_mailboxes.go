package migrations

// The local copy of each email config's mail: its mailboxes, and a row per message holding what
// a list shows. Bodies arrive in a migration of their own.
var mailboxes = Migration{
	Name: "20261001133306_mailboxes",
	Up: exec(`
ALTER TABLE email_configs ADD COLUMN synced_at INTEGER;
ALTER TABLE email_configs ADD COLUMN sync_error TEXT NOT NULL DEFAULT '';

CREATE TABLE mailboxes (
  id              TEXT PRIMARY KEY,
  email_config_id TEXT NOT NULL REFERENCES email_configs (id) ON DELETE CASCADE,
  -- The server's own name, decoded: what is sent back to it, never shown raw.
  name            TEXT NOT NULL,
  delimiter       TEXT NOT NULL DEFAULT '',
  special_use     TEXT NOT NULL DEFAULT '',
  selectable      INTEGER NOT NULL DEFAULT 1,
  -- What the server said at the last look. Zero until the first one.
  uidvalidity     INTEGER NOT NULL DEFAULT 0,
  uidnext         INTEGER NOT NULL DEFAULT 0,
  messages        INTEGER NOT NULL DEFAULT 0,
  unseen          INTEGER NOT NULL DEFAULT 0,
  synced_at       INTEGER,
  UNIQUE (email_config_id, name)
);

-- A message is keyed by its UID within its mailbox, and named outside by id. The UIDVALIDITY
-- they belong to is the mailbox's: when it changes, every row here is dropped together.
CREATE TABLE messages (
  id              TEXT PRIMARY KEY,
  mailbox_id      TEXT NOT NULL REFERENCES mailboxes (id) ON DELETE CASCADE,
  uid             INTEGER NOT NULL,
  internal_date   INTEGER NOT NULL,
  -- The Date header, which the sender wrote and can lie about. NULL when there is none.
  date            INTEGER,
  size            INTEGER NOT NULL DEFAULT 0,
  seen            INTEGER NOT NULL DEFAULT 0,
  flagged         INTEGER NOT NULL DEFAULT 0,
  answered        INTEGER NOT NULL DEFAULT 0,
  draft           INTEGER NOT NULL DEFAULT 0,
  subject         TEXT NOT NULL DEFAULT '',
  from_name       TEXT NOT NULL DEFAULT '',
  from_email      TEXT NOT NULL DEFAULT '',
  -- JSON arrays of {"name","email"}: shown whole and never queried into.
  to_json         TEXT NOT NULL DEFAULT '[]',
  cc_json         TEXT NOT NULL DEFAULT '[]',
  message_id      TEXT NOT NULL DEFAULT '',
  in_reply_to     TEXT NOT NULL DEFAULT '',
  has_attachments INTEGER NOT NULL DEFAULT 0,
  preview         TEXT NOT NULL DEFAULT '',
  UNIQUE (mailbox_id, uid)
);
-- The list: newest to arrive first.
CREATE INDEX messages_list ON messages (mailbox_id, internal_date DESC, uid DESC);
`),
}
