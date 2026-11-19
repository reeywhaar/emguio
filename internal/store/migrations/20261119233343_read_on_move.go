package migrations

// Whether mail archived or deleted is marked read first: the user's choice for every email
// config, and one config's own, NULL where it follows the user's. See docs/reading.md.
var readOnMove = Migration{
	Name: "20261119233343_read_on_move",
	Up: exec(`
ALTER TABLE users ADD COLUMN read_on_archive INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN read_on_trash INTEGER NOT NULL DEFAULT 0;
ALTER TABLE email_configs ADD COLUMN read_on_archive INTEGER;
ALTER TABLE email_configs ADD COLUMN read_on_trash INTEGER;
`),
}
