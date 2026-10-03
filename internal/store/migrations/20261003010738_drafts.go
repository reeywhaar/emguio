package migrations

// What somebody is writing, kept here between saves and written to the mail server's Drafts
// now and then, with the files it carries. See docs/sending.md.
var drafts = Migration{
	Name: "20261003010738_drafts",
	Up: exec(`
CREATE TABLE drafts (
  id              TEXT PRIMARY KEY,
  user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  email_config_id TEXT NOT NULL REFERENCES email_configs (id) ON DELETE CASCADE,
  -- The address fields as typed.
  to_field        TEXT NOT NULL DEFAULT '',
  cc_field        TEXT NOT NULL DEFAULT '',
  bcc_field       TEXT NOT NULL DEFAULT '',
  subject         TEXT NOT NULL DEFAULT '',
  body            TEXT NOT NULL DEFAULT '',
  -- The message it answers, marked answered once it is sent, and the headers that thread it.
  reply_mailbox   TEXT NOT NULL DEFAULT '',
  reply_message   TEXT NOT NULL DEFAULT '',
  in_reply_to     TEXT NOT NULL DEFAULT '',
  refs            TEXT NOT NULL DEFAULT '',
  -- Where the mail server holds it as last written, empty before that.
  kept_mailbox    TEXT NOT NULL DEFAULT '',
  kept_message    TEXT NOT NULL DEFAULT '',
  -- Each save counts up version; written is the version last written to the mail server.
  version         INTEGER NOT NULL DEFAULT 1,
  written         INTEGER NOT NULL DEFAULT 0,
  -- When it is next written, 0 for not due; how many tries since it last was.
  due_at          INTEGER NOT NULL DEFAULT 0,
  attempts        INTEGER NOT NULL DEFAULT 0,
  -- Set once its window is closed: it goes from here once written.
  closed          INTEGER NOT NULL DEFAULT 0,
  -- Why it could not be written, the last time it was tried.
  problem         TEXT NOT NULL DEFAULT '',
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);
CREATE INDEX drafts_due ON drafts (due_at) WHERE due_at > 0;
CREATE INDEX drafts_user ON drafts (user_id);

CREATE TABLE draft_parts (
  id       TEXT PRIMARY KEY,
  draft_id TEXT NOT NULL REFERENCES drafts (id) ON DELETE CASCADE,
  position INTEGER NOT NULL,
  name     TEXT NOT NULL,
  type     TEXT NOT NULL,
  data     BLOB NOT NULL
);
CREATE INDEX draft_parts_draft ON draft_parts (draft_id, position);
`),
}
