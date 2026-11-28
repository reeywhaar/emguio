package migrations

// Whether mail marked as spam is marked read first, as archived and deleted mail can be: the
// user's choice for every email config, and one config's own, NULL where it follows the user's.
// See docs/reading.md.
var readOnJunk = Migration{
	Name: "20261128082140_read_on_junk",
	Up: exec(`
ALTER TABLE users ADD COLUMN read_on_junk INTEGER NOT NULL DEFAULT 0;
ALTER TABLE email_configs ADD COLUMN read_on_junk INTEGER;
`),
}
