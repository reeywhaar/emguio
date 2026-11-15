package migrations

// How a user's notifications come: without a sound, and not at all in their quiet hours, which
// are minutes after midnight in a time zone. See docs/push.md.
var pushSettings = Migration{
	Name: "20261115120100_push_settings",
	Up: exec(`
ALTER TABLE users ADD COLUMN push_silent INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN quiet_from INTEGER;
ALTER TABLE users ADD COLUMN quiet_until INTEGER;
ALTER TABLE users ADD COLUMN quiet_zone TEXT NOT NULL DEFAULT '';
`),
}
