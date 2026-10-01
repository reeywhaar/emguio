package migrations

// What is kept of a config's mail is INBOX's newest messages, the window, and no bodies at all;
// everything else is read from the server when it is asked for. A message row is keyed by its
// UID alone, as the API names it. What was copied before goes, and the next pass fills the
// window again: synced_at goes back to NULL so the list asks the server until it has.
var window = Migration{
	Name: "20261001172003_window",
	Up: exec(`
DROP TABLE message_bodies;
DROP TABLE messages;

CREATE TABLE messages (
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
  PRIMARY KEY (mailbox_id, uid)
) WITHOUT ROWID;

UPDATE mailboxes SET synced_at = NULL;
`),
}
