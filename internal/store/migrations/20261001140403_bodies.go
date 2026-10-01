package migrations

// A message as the server sent it, kept once it has been opened, so the next opening and every
// attachment come from here rather than from the server again.
var bodies = Migration{
	Name: "20261001140403_bodies",
	Up: exec(`
CREATE TABLE message_bodies (
  message_id TEXT PRIMARY KEY REFERENCES messages (id) ON DELETE CASCADE,
  raw        BLOB NOT NULL,
  fetched_at INTEGER NOT NULL
) WITHOUT ROWID;
`),
}
