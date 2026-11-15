package migrations

// A move of messages to another of the user's accounts: one job per message, and a row that
// says how far it has got. See docs/reading.md.
var transfers = Migration{
	Name: "20261115070000_transfers",
	Up: exec(`
CREATE TABLE transfers (
  id                TEXT PRIMARY KEY,
  user_id           TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  email_config_id   TEXT NOT NULL REFERENCES email_configs (id) ON DELETE CASCADE,
  mailbox_id        TEXT NOT NULL REFERENCES mailboxes (id) ON DELETE CASCADE,
  target_config_id  TEXT NOT NULL REFERENCES email_configs (id) ON DELETE CASCADE,
  target_mailbox_id TEXT NOT NULL REFERENCES mailboxes (id) ON DELETE CASCADE,
  total             INTEGER NOT NULL,
  moved             INTEGER NOT NULL DEFAULT 0,
  cancelled         INTEGER NOT NULL DEFAULT 0,
  -- Non-empty once a server refused what was left: the sentence.
  error             TEXT NOT NULL DEFAULT '',
  created_at        INTEGER NOT NULL
);
CREATE INDEX transfers_user ON transfers (user_id);
-- The transfer a job is one message of; empty for any other job.
ALTER TABLE jobs ADD COLUMN transfer TEXT NOT NULL DEFAULT '';
CREATE INDEX jobs_transfer ON jobs (transfer) WHERE transfer <> '';
`),
}
