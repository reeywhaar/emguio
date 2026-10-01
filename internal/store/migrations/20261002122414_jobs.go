package migrations

// What somebody asked to be done to a message, kept until the server has done it: taken at
// once, done in order, and done whether or not the page that asked is still open.
var jobs = Migration{
	Name: "20261002122414_jobs",
	Up: exec(`
CREATE TABLE jobs (
  id              TEXT PRIMARY KEY,
  user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  email_config_id TEXT NOT NULL REFERENCES email_configs (id) ON DELETE CASCADE,
  mailbox_id      TEXT NOT NULL REFERENCES mailboxes (id) ON DELETE CASCADE,
  -- The message as the API names it, {uidvalidity}-{uid}.
  message         TEXT NOT NULL,
  -- seen, flagged, move or delete.
  kind            TEXT NOT NULL,
  -- For seen and flagged, set or cleared.
  value           INTEGER NOT NULL DEFAULT 0,
  -- For move, the mailbox it goes to.
  target          TEXT NOT NULL DEFAULT '',
  -- Whether it was read when asked, as the asker drew it: what its folders' counts move by.
  seen            INTEGER NOT NULL DEFAULT 1,
  -- How many times it has been tried, and when it may be tried next.
  attempts        INTEGER NOT NULL DEFAULT 0,
  next_at         INTEGER NOT NULL DEFAULT 0,
  -- Non-empty once it has failed for good: the sentence, kept until the user has seen it.
  error           TEXT NOT NULL DEFAULT '',
  created_at      INTEGER NOT NULL
);
-- A config's queue, in the order asked: by rowid, which two jobs asked in one millisecond do not
-- share, where their ULIDs would sort by chance.
CREATE INDEX jobs_queue ON jobs (email_config_id);
CREATE INDEX jobs_user ON jobs (user_id);
`),
}
